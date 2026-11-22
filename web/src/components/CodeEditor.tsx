import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
  snippetCompletion,
  type Completion,
  type CompletionContext,
} from '@codemirror/autocomplete'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, HighlightStyle, indentUnit, StreamLanguage, syntaxHighlighting } from '@codemirror/language'
import { Prec } from '@codemirror/state'
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  hoverTooltip,
  keymap,
  lineNumbers,
  placeholder as cmPlaceholder,
  tooltips,
  type Tooltip,
} from '@codemirror/view'
import { tags } from '@lezer/highlight'
import CodeMirror, { type ReactCodeMirrorRef } from '@uiw/react-codemirror'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import type { HelpDoc } from '../types'

/**
 * CodeEditor：基于 CodeMirror 6 的 DSL 编辑器。
 *
 * 自研「透明 textarea + 高亮层」在 Windows 缩放下会出现点击位置与光标错位；
 * CodeMirror 用单一文档视图量字，光标、选区和点击落在同一坐标系。
 * 输入时弹出字段 / 算子补全，悬停显示签名与说明。
 */

export interface CodeEditorHandle {
  insert: (text: string) => void
  focus: () => void
}

type Operator = { name: string; min_args: number; max_args: number }

interface Props {
  value: string
  onChange: (v: string) => void
  fields: string[]
  operators: Operator[]
  onRun?: () => void
  status?: 'idle' | 'ok' | 'error'
  placeholder?: string
  minHeight?: number
  fill?: boolean
}

const KEYWORDS = new Set(['and', 'or', 'not'])

function wordAt(text: string, offset: number): { from: number; to: number; word: string } | null {
  const re = /[A-Za-z_][A-Za-z0-9_]*/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    if (offset >= m.index && offset <= m.index + m[0].length) {
      return { from: m.index, to: m.index + m[0].length, word: m[0] }
    }
  }
  return null
}

function snippetFromSignature(name: string, signature: string | undefined, minArgs: number): string {
  const inner = signature?.replace(/^[^(]+\(/, '').replace(/\)$/, '').trim()
  if (inner) {
    const args = inner.split(',').map((s) => s.trim()).filter(Boolean)
    if (args.length) return `${name}(${args.map((a) => `\${${a}}`).join(', ')})`
    return `${name}()`
  }
  if (minArgs <= 0) return `${name}()`
  const names = ['x', 'window', 'y']
  const args = Array.from({ length: minArgs }, (_, i) => `\${${names[i] ?? `arg${i + 1}`}}`)
  return `${name}(${args.join(', ')})`
}

function dslLanguage(fieldSet: Set<string>, opSet: Set<string>) {
  return StreamLanguage.define({
    name: 'factor-dsl',
    startState: () => ({}),
    token(stream) {
      if (stream.eatSpace()) return null
      if (stream.match(/#[^\n]*/)) return 'comment'
      if (stream.match(/\d+(?:\.\d+)?/)) return 'number'
      if (stream.match(/[A-Za-z_][A-Za-z0-9_]*/)) {
        const w = stream.current()
        if (opSet.has(w)) return 'macroName'
        if (fieldSet.has(w)) return 'typeName'
        if (KEYWORDS.has(w)) return 'keyword'
        return 'variableName'
      }
      if (stream.match(/[+\-*/=<>!]+/)) return 'operator'
      if (stream.match(/[(),]/)) return 'punctuation'
      stream.next()
      return null
    },
  })
}

const dslHighlight = HighlightStyle.define([
  { tag: tags.comment, color: 'var(--sub)', fontStyle: 'italic' },
  { tag: tags.keyword, color: 'var(--warn)' },
  { tag: tags.number, color: 'var(--info-fg)' },
  { tag: tags.macroName, color: 'var(--purple-fg)' },
  { tag: tags.typeName, color: 'var(--ok)' },
  { tag: tags.variableName, color: 'var(--ink)' },
  { tag: tags.operator, color: 'var(--sub)' },
  { tag: tags.punctuation, color: 'var(--sub)' },
])

/** tooltips 挂到独立节点，避免主题里的 height:100% 把 body 撑出一屏空白。 */
function cmTooltipParent(): HTMLElement {
  const id = 'cm-tooltip-root'
  let el = document.getElementById(id)
  if (!el) {
    el = document.createElement('div')
    el.id = id
    document.body.appendChild(el)
  }
  return el
}

const dslTheme = EditorView.theme({
  '&': { backgroundColor: 'var(--sheet)', color: 'var(--ink)', fontSize: '12.5px' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': {
    fontFamily: "var(--mono), 'JetBrains Mono', Consolas, 'Courier New', monospace",
    lineHeight: '1.85',
  },
  '.cm-content': { caretColor: 'var(--ink)', padding: '8px 0' },
  '.cm-line': { padding: '0 14px 0 8px' },
  '.cm-gutters': {
    backgroundColor: 'var(--panel)',
    color: 'var(--sub)',
    border: 'none',
    borderRight: '1px solid var(--line)',
  },
  '.cm-lineNumbers .cm-gutterElement': { padding: '0 8px 0 10px', minWidth: '36px' },
  '.cm-activeLine': { backgroundColor: 'var(--hover)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--hover)', color: 'var(--ink-2)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'color-mix(in srgb, var(--ink) 16%, transparent)',
  },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--ink)' },
  '.cm-placeholder': { color: 'var(--sub)' },
})

function hintCard(title: string, desc?: string, extra?: string): HTMLElement {
  const wrap = document.createElement('div')
  wrap.className = 'cm-hint-card'
  const sig = document.createElement('div')
  sig.className = 'cm-hint-sig'
  sig.textContent = title
  wrap.appendChild(sig)
  if (desc) {
    const d = document.createElement('div')
    d.className = 'cm-hint-desc'
    d.textContent = desc
    wrap.appendChild(d)
  }
  if (extra) {
    const e = document.createElement('div')
    e.className = 'cm-hint-ex'
    e.textContent = extra
    wrap.appendChild(e)
  }
  return wrap
}

function buildCompletions(fields: string[], operators: Operator[], help: HelpDoc | null): Completion[] {
  const fieldDoc = new Map((help?.fields ?? []).map((f) => [f.name, f]))
  const opDoc = new Map((help?.operators ?? []).map((o) => [o.name, o]))
  const out: Completion[] = []

  for (const name of fields) {
    const d = fieldDoc.get(name)
    out.push({
      label: name,
      type: 'variable',
      detail: '字段',
      info: d ? () => hintCard(name, d.description, d.tip) : undefined,
      boost: 2,
    })
  }

  for (const op of operators) {
    const d = opDoc.get(op.name)
    out.push(
      snippetCompletion(snippetFromSignature(op.name, d?.signature, op.min_args), {
        label: op.name,
        type: 'function',
        detail: d?.category ?? '算子',
        info: d ? () => hintCard(d.signature || op.name, d.description, d.example ? `例：${d.example}` : d.tip) : undefined,
        boost: 3,
      }),
    )
  }

  for (const kw of KEYWORDS) {
    out.push({ label: kw, type: 'keyword', detail: '关键字', boost: 1 })
  }
  return out
}

function completionSource(options: Completion[]) {
  return (context: CompletionContext) => {
    const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_]*/)
    if (!word && !context.explicit) {
      const ch = context.state.sliceDoc(Math.max(0, context.pos - 1), context.pos)
      if (!/[(,=]/.test(ch)) return null
    }
    return {
      from: word ? word.from : context.pos,
      options,
      validFor: /^[A-Za-z_][A-Za-z0-9_]*$/,
    }
  }
}

function hoverSource(fields: string[], operators: Operator[], help: HelpDoc | null) {
  const fieldSet = new Set(fields)
  const opMap = new Map(operators.map((o) => [o.name, o]))
  const fieldDoc = new Map((help?.fields ?? []).map((f) => [f.name, f]))
  const opDoc = new Map((help?.operators ?? []).map((o) => [o.name, o]))

  return hoverTooltip((view, pos): Tooltip | null => {
    const line = view.state.doc.lineAt(pos)
    const found = wordAt(line.text, pos - line.from)
    if (!found) return null
    const name = found.word
    let dom: HTMLElement | null = null
    const op = opMap.get(name)
    const opD = opDoc.get(name)
    const fieldD = fieldDoc.get(name)
    if (op || opD) {
      const sig = opD?.signature ?? `${name}(${op ? `${op.min_args}~${op.max_args} 参` : ''})`
      dom = hintCard(sig, opD?.description, opD?.example ? `例：${opD.example}` : opD?.tip)
    } else if (fieldSet.has(name) || fieldD) {
      dom = hintCard(name, fieldD?.description ?? '数据字段', fieldD?.tip)
    } else if (KEYWORDS.has(name)) {
      dom = hintCard(name, '逻辑关键字')
    }
    if (!dom) return null
    return {
      pos: line.from + found.from,
      end: line.from + found.to,
      above: true,
      create: () => ({ dom }),
    }
  })
}

export const CodeEditor = forwardRef<CodeEditorHandle, Props>(function CodeEditor(
  { value, onChange, fields, operators, onRun, status = 'idle', placeholder, minHeight = 200, fill = false },
  ref,
) {
  const cmRef = useRef<ReactCodeMirrorRef>(null)
  const onRunRef = useRef(onRun)
  onRunRef.current = onRun
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  const posRef = useRef<HTMLSpanElement>(null)
  const [help, setHelp] = useState<HelpDoc | null>(null)

  // @uiw/react-codemirror 会在 onChange / onUpdate 引用变化时 StateEffect.reconfigure
  // 整份扩展。拖选跨行会连续触发 selectionSet；若此时用内联 onUpdate + setState，
  // 手势中途被重配掐断，选区就停在起始行。回调必须保持引用稳定，行/列用 DOM 更新。
  const emitChange = useCallback((v: string) => {
    onChangeRef.current(v)
  }, [])

  useEffect(() => {
    api.help().then(setHelp).catch(() => {})
  }, [])

  useImperativeHandle(ref, () => ({
    insert(text: string) {
      const view = cmRef.current?.view
      if (!view) return
      const { from, to } = view.state.selection.main
      view.dispatch({
        changes: { from, to, insert: text },
        selection: { anchor: from + text.length },
      })
      view.focus()
    },
    focus() {
      cmRef.current?.view?.focus()
    },
  }))

  const extensions = useMemo(() => {
    const fieldSet = new Set(fields)
    const opSet = new Set(operators.map((o) => o.name))
    const options = buildCompletions(fields, operators, help)
    return [
      dslLanguage(fieldSet, opSet),
      syntaxHighlighting(dslHighlight),
      dslTheme,
      lineNumbers(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      highlightSpecialChars(),
      drawSelection(),
      bracketMatching(),
      closeBrackets(),
      history(),
      indentUnit.of('  '),
      tooltips({ parent: cmTooltipParent() }),
      autocompletion({
        override: [completionSource(options)],
        icons: false,
        closeOnBlur: true,
        activateOnTyping: true,
      }),
      hoverSource(fields, operators, help),
      keymap.of([
        ...closeBracketsKeymap,
        indentWithTab,
        ...historyKeymap,
        ...defaultKeymap,
      ]),
      Prec.high(keymap.of(completionKeymap)),
      Prec.high(
        keymap.of([
          {
            key: 'Mod-Enter',
            run: () => {
              onRunRef.current?.()
              return true
            },
          },
        ]),
      ),
      placeholder ? cmPlaceholder(placeholder) : [],
      EditorView.updateListener.of((u) => {
        if (!u.selectionSet && !u.docChanged) return
        const head = u.state.selection.main.head
        const line = u.state.doc.lineAt(head)
        const el = posRef.current
        if (el) el.textContent = `行 ${line.number}, 列 ${head - line.from + 1}`
      }),
    ]
  }, [fields, operators, help, placeholder])

  const statusLabel =
    status === 'ok' ? { text: '✓ 校验通过', cls: 'ok' } : status === 'error' ? { text: '✗ 校验未通过', cls: 'err' } : { text: '未校验', cls: '' }

  return (
    <div className={`cm-wrap${fill ? ' fill' : ''}`}>
      <div className="cm-body" style={fill ? undefined : { minHeight }}>
        <CodeMirror
          ref={cmRef}
          value={value}
          onChange={emitChange}
          extensions={extensions}
          basicSetup={false}
          theme="none"
          height="100%"
        />
      </div>
      <div className="cm-status">
        <span ref={posRef}>行 1, 列 1</span>
        <span>{value.length} 字符</span>
        <span className={`cm-status-${statusLabel.cls}`}>{statusLabel.text}</span>
        <span className="cm-status-hint">Ctrl+Space 补全 · Ctrl+Enter 评价</span>
      </div>
    </div>
  )
})
