import {
  Children,
  createContext,
  isValidElement,
  useContext,
  useState,
  type ReactNode,
} from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

export interface HelpNav {
  openGuide: (id: string) => void
  jump: (id: string) => void
}

export const HelpNavContext = createContext<HelpNav | null>(null)

type MdNode = { type?: string; value?: string; children?: MdNode[] }

function walk(node: MdNode | undefined, visit: (n: MdNode) => void) {
  if (!node) return
  visit(node)
  node.children?.forEach((child) => walk(child, visit))
}

/** GitHub 风格提示块：> [!NOTE] / [!TIP] / [!WARNING] / [!CAUTION] */
function remarkHelpAlerts() {
  return (tree: MdNode) => {
    walk(tree, (node) => {
      if (node.type !== 'blockquote' || !node.children?.length) return
      const para = node.children[0]
      if (para.type !== 'paragraph' || !para.children?.length) return
      const first = para.children[0]
      if (first.type !== 'text' || typeof first.value !== 'string') return
      const m = first.value.match(/^\[!(NOTE|TIP|INFO|WARNING|CAUTION)\][ \t]*(?:\r?\n)?/)
      if (!m) return
      first.value = first.value.slice(m[0].length)
      if (!first.value) para.children.shift()
      if (!para.children.length) node.children?.shift()
      const warn = m[1] === 'WARNING' || m[1] === 'CAUTION'
      Object.assign(node, {
        data: { hProperties: { className: warn ? 'help-alert-warn' : 'help-alert-info' } },
      })
    })
  }
}

function codeText(children: ReactNode): string {
  const child = Children.toArray(children)[0]
  if (isValidElement<{ children?: ReactNode }>(child)) {
    return String(child.props.children ?? '').replace(/\n$/, '')
  }
  return String(children ?? '').replace(/\n$/, '')
}

function codeLang(children: ReactNode): string {
  const child = Children.toArray(children)[0]
  if (!isValidElement<{ className?: string }>(child)) return ''
  return String(child.props.className || '').replace(/^language-/, '')
}

function HelpLink({ href = '', children }: { href?: string; children?: ReactNode }) {
  const nav = useContext(HelpNavContext)
  if (href.startsWith('#guide-')) {
    const id = href.slice('#guide-'.length)
    return (
      <button type="button" className="help-link" onClick={() => nav?.openGuide(id)}>
        {children}
      </button>
    )
  }
  if (href.startsWith('#') && !href.startsWith('#/')) {
    return (
      <button type="button" className="help-link" onClick={() => nav?.jump(href.slice(1))}>
        {children}
      </button>
    )
  }
  return (
    <a className="help-link" href={href}>
      {children}
    </a>
  )
}

export function CopyButton({ text, label = '复制' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }
  return (
    <button className="btn xs" onClick={copy}>
      {copied ? '已复制' : label}
    </button>
  )
}

const components: Components = {
  a: HelpLink,
  table({ children }) {
    return (
      <div className="tbl-wrap">
        <table className="tbl">{children}</table>
      </div>
    )
  },
  blockquote({ className, children }) {
    if (className?.includes('help-alert-warn')) {
      return (
        <div className="note warn">
          <span>!</span>
          <div>{children}</div>
        </div>
      )
    }
    if (className?.includes('help-alert-info')) {
      return (
        <div className="note info">
          <span>i</span>
          <div>{children}</div>
        </div>
      )
    }
    return <blockquote>{children}</blockquote>
  },
  pre({ children }) {
    const text = codeText(children)
    const lang = codeLang(children)
    const labeled = lang.includes('.')
    return (
      <div className={labeled ? 'help-code' : 'help-md-code'}>
        {labeled && (
          <div className="help-code-head">
            <span>{lang}</span>
            <CopyButton text={text} />
          </div>
        )}
        <pre>{text}</pre>
        {!labeled && <CopyButton text={text} />}
      </div>
    )
  },
  code({ className, children }) {
    if (className) return <code className={className}>{children}</code>
    return <code>{children}</code>
  },
}

export function interpolate(src: string, vars: Record<string, string>) {
  return src.replace(/\{\{([a-zA-Z0-9_.]+)\}\}/g, (_, key: string) => vars[key] ?? '')
}

export function HelpMarkdown({ source, vars }: { source: string; vars?: Record<string, string> }) {
  const text = vars ? interpolate(source, vars) : source
  return (
    <div className="help-md">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkHelpAlerts]} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  )
}
