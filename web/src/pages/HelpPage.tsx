import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { CATEGORY_LABEL } from '../components/Layout'
import {
  ERROR_HINTS,
  FAQS,
  FAQ_GROUPS,
  GLOSSARY,
  GLOSSARY_GROUPS,
  GUIDES,
  START_MD,
  SYNTAX_MD,
} from '../help/catalog'
import { CopyButton, HelpMarkdown, HelpNavContext, interpolate } from '../help/Markdown'
import type { HelpDoc } from '../types'

const sections = [
  ['start', '新手上路'],
  ['guides', '入门教程'],
  ['glossary', '名词解释'],
  ['syntax', 'DSL 语法'],
  ['fields', '基础指标'],
  ['operators', '算子参考'],
  ['builtins', '内置因子'],
  ['faq', '常见问题'],
] as const

const OPERATOR_ORDER = ['时序', '截面', '数学', '条件']

function operatorRank(category: string) {
  const i = OPERATOR_ORDER.indexOf(category)
  return i < 0 ? OPERATOR_ORDER.length : i
}

function includes(q: string, ...values: (string | undefined)[]) {
  return !q || values.some((value) => value?.toLowerCase().includes(q))
}

function groupBy<T>(items: T[], keyOf: (item: T) => string) {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const key = keyOf(item)
    groups.set(key, [...(groups.get(key) ?? []), item])
  }
  return groups
}

export function HelpPage() {
  const [doc, setDoc] = useState<HelpDoc | null>(null)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')

  useEffect(() => {
    api.help().then(setDoc).catch((e) => setError((e as Error).message))
  }, [])

  const q = query.trim().toLowerCase()
  const syntaxVars = useMemo((): Record<string, string> => {
    if (!doc) return {}
    return {
      'dsl.result': doc.dsl.result,
      'dsl.separators': doc.dsl.statement_separators.join(' 或 '),
      'dsl.comment': doc.dsl.comment,
      'dsl.max_window': String(doc.dsl.max_window),
      'dsl.max_depth': String(doc.dsl.max_depth),
      'dsl.precedence': doc.dsl.precedence.map((item) => '`' + item + '`').join(' '),
    }
  }, [doc])

  const filtered = useMemo(() => {
    if (!doc) return null
    const syntaxText = interpolate(SYNTAX_MD, syntaxVars)
    const startText = interpolate(START_MD, {
      data_status: doc.fields.length > 0 ? '当前已就绪' : '当前尚未加载',
    })
    return {
      guides: GUIDES.filter((x) => includes(q, x.title, x.summary, x.level, x.keywords, x.body)),
      terms: GLOSSARY.filter((x) => includes(q, x.term, x.alias, x.body)),
      fields: doc.fields.filter((x) => includes(q, x.name, x.description, x.unit, x.tip)),
      operators: doc.operators.filter((x) => includes(q, x.name, x.category, x.signature, x.description, x.example, x.tip)),
      builtins: doc.builtins.filter((x) => includes(q, x.name, x.category, x.description, x.expr, CATEGORY_LABEL[x.category])),
      faqs: FAQS.filter((x) => includes(q, x.q, x.keywords, x.body)),
      errors: ERROR_HINTS.filter((x) => includes(q, x.msg, x.meaning, x.fix)),
      start: includes(q, startText),
      syntax: includes(q, syntaxText),
    }
  }, [doc, q, syntaxVars])

  const jump = (id: string) => {
    if (query) setQuery('')
    requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
  }

  const openGuide = (id: string) => {
    if (query) setQuery('')
    requestAnimationFrame(() => {
      const el = document.getElementById(`guide-${id}`)
      if (el instanceof HTMLDetailsElement) el.open = true
      el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    })
  }

  if (error) return <div className="note err">帮助文档加载失败：{error}</div>
  if (!doc || !filtered) return <div className="busy-line"><span className="spinner" />正在加载当前服务能力…</div>

  const operatorGroups = [...groupBy(filtered.operators, (item) => item.category).entries()].sort(
    ([a], [b]) => operatorRank(a) - operatorRank(b),
  )
  const builtinGroups = groupBy(filtered.builtins, (item) => item.category)
  const dataReady = doc.fields.length > 0

  const show = {
    start: filtered.start,
    guides: filtered.guides.length > 0,
    glossary: filtered.terms.length > 0,
    syntax: filtered.syntax,
    fields: !q || filtered.fields.length > 0,
    operators: filtered.operators.length > 0,
    builtins: filtered.builtins.length > 0,
    faq: filtered.faqs.length > 0 || filtered.errors.length > 0,
  }
  const hits =
    filtered.guides.length + filtered.terms.length + filtered.fields.length + filtered.operators.length +
    filtered.builtins.length + filtered.faqs.length + filtered.errors.length +
    (show.start ? 1 : 0) + (show.syntax ? 1 : 0)
  const nothing = !!q && hits === 0

  return (
    <HelpNavContext.Provider value={{ openGuide, jump }}>
      <div className="help-page">
        <aside className="help-toc" aria-label="文档目录">
          <div className="help-toc-title">本文档</div>
          {sections.map(([id, label]) => (
            <button key={id} onClick={() => jump(id)}>{label}</button>
          ))}
          <div className="help-toc-meta">
            <b>{GUIDES.length}</b> 篇教程 · <b>{GLOSSARY.length}</b> 个名词<br />
            <b>{doc.fields.length}</b> 个当前字段<br />
            <b>{doc.operators.length}</b> 个可调用算子<br />
            <b>{doc.builtins.length}</b> 个内置因子
          </div>
        </aside>

        <main className="help-doc">
          <div className="help-hero">
            <div>
              <span className="help-eyebrow">FACTOR LAB · HELP</span>
              <h1>帮助文档</h1>
              <p>不需要量化基础：从“因子是什么”讲起，带你跑通第一个因子，再到公式写法、指标含义、名词解释和常见问题的完整参考。</p>
            </div>
            <span className="tag green">随当前服务动态更新</span>
          </div>

          <label className="help-search">
            <span>⌕</span>
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索教程、名词、字段、算子、报错或常见问题…"
              aria-label="搜索帮助文档"
            />
            {query && <button onClick={() => setQuery('')} aria-label="清空搜索">×</button>}
          </label>

          {q && !nothing && <p className="help-result">找到 <b>{hits}</b> 条与“{query.trim()}”相关的内容，已隐藏无关小节。</p>}
          {nothing && (
            <div className="help-empty">
              没有找到与“{query.trim()}”相关的内容。试试换一个更短的词，例如“IC”“窗口”“换手”“报错”。
            </div>
          )}

          {show.start && (
            <section id="start" className="help-section">
              <h2>新手上路</h2>
              <HelpMarkdown
                source={START_MD}
                vars={{ data_status: dataReady ? '**当前已就绪**' : '**当前尚未加载**' }}
              />
            </section>
          )}

          {show.guides && (
            <section id="guides" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>入门教程</h2>
                  <p>建议按顺序阅读：先理解因子研究在做什么，再学会写公式、读报告，然后是 AI 助手、因子库、组合与模拟盘的用法。点击标题展开。</p>
                </div>
                <span className="help-count">{GUIDES.reduce((n, g) => n + g.minutes, 0)} 分钟</span>
              </div>
              <div className="help-guide-list">
                {filtered.guides.map((guide) => (
                  <details key={guide.id} id={`guide-${guide.id}`} className="help-guide" open={guide.id === GUIDES[0].id && !q}>
                    <summary>
                      <span className="help-guide-no">{String(GUIDES.findIndex((g) => g.id === guide.id) + 1).padStart(2, '0')}</span>
                      <span className="help-guide-main">
                        <b>{guide.title}</b>
                        <span>{guide.summary}</span>
                      </span>
                      <span className="help-guide-meta">
                        <span className={`tag ${guide.level === '入门' ? 'blue' : 'purple'}`}>{guide.level}</span>
                        <i>{guide.minutes} 分钟</i>
                      </span>
                    </summary>
                    <article className="help-guide-body">
                      <HelpMarkdown source={guide.body} />
                    </article>
                  </details>
                ))}
              </div>
            </section>
          )}

          {show.glossary && (
            <section id="glossary" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>名词解释</h2>
                  <p>页面和教程里出现的专业词汇，用一两句大白话解释。记不住没关系，用到时回来查。</p>
                </div>
                <span className="help-count">{filtered.terms.length} / {GLOSSARY.length}</span>
              </div>
              {GLOSSARY_GROUPS.map((group) => {
                const items = filtered.terms.filter((t) => t.group === group)
                if (!items.length) return null
                return (
                  <div className="help-group" key={group}>
                    <h3>{group}<span>{items.length}</span></h3>
                    <dl className="help-term-grid">
                      {items.map((t) => (
                        <div className="help-term" key={t.term}>
                          <dt>{t.term}</dt>
                          <dd><HelpMarkdown source={t.body} /></dd>
                        </div>
                      ))}
                    </dl>
                  </div>
                )
              })}
            </section>
          )}

          {show.syntax && (
            <section id="syntax" className="help-section">
              <h2>DSL 语法</h2>
              <HelpMarkdown source={SYNTAX_MD} vars={syntaxVars} />
            </section>
          )}

          {show.fields && (
            <section id="fields" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>基础指标</h2>
                  <p>平台直接提供的原始数据，写公式时直接用名字引用。内容来自当前加载的数据，切换 Tushare 或 CSV 后会自动变化。</p>
                </div>
                <span className="help-count">{filtered.fields.length} / {doc.fields.length}</span>
              </div>
              {filtered.fields.length ? (
                <div className="tbl-wrap">
                  <table className="tbl">
                    <thead><tr><th>字段</th><th>含义</th><th>单位</th></tr></thead>
                    <tbody>{filtered.fields.map((field) => (
                      <tr key={field.name}>
                        <td><code className="inline">{field.name}</code></td>
                        <td>
                          {field.description}
                          {field.tip && <div className="help-tip">{field.tip}</div>}
                        </td>
                        <td className="nowrap">{field.unit || '由数据源定义'}</td>
                      </tr>
                    ))}</tbody>
                  </table>
                </div>
              ) : (
                <div className="help-empty">当前还没有加载行情数据，所以没有可用字段。请先在左下角「设置」里填写 Tushare Token。</div>
              )}
            </section>
          )}

          {show.operators && (
            <section id="operators" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>算子参考</h2>
                  <p>
                    算子就是现成的计算函数。名字的前缀说明它往哪个方向算：<code>ts_</code> 是一只股票和自己的过去比，<code>cs_</code> 是同一天和其他股票比。括号里的 <code>x</code>、<code>y</code> 可以填字段、变量或另一段公式，<code>window</code> 填往回看的交易日数。
                  </p>
                </div>
                <span className="help-count">{filtered.operators.length} / {doc.operators.length}</span>
              </div>
              {operatorGroups.map(([category, items]) => (
                <div className="help-group" key={category}>
                  <h3>{category}<span>{items.length}</span></h3>
                  <div className="help-op-grid">
                    {items.map((op) => (
                      <article className="help-op" key={op.name}>
                        <div className="help-op-head"><code>{op.signature}</code><CopyButton text={op.example} label="复制示例" /></div>
                        <p>{op.description}</p>
                        {op.tip && <div className="help-tip">{op.tip}</div>}
                        <pre>{op.example}</pre>
                      </article>
                    ))}
                  </div>
                </div>
              ))}
            </section>
          )}

          {show.builtins && (
            <section id="builtins" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>因子库内置指标</h2>
                  <p>
                    平台预置的经典因子，来自公开的研究成果（如 WorldQuant 的 Alpha101、微软 Qlib 的 Alpha158）。点击任意一行展开公式，可以复制到「因子工厂」里运行或改写，是学习公式写法的好材料。这份目录随版本发布，不受你对因子库的编辑或删除影响。
                  </p>
                </div>
                <span className="help-count">{filtered.builtins.length} / {doc.builtins.length}</span>
              </div>
              <div className="note warn mb12"><span>!</span><div>内置因子是研究模板，不构成投资建议。能否使用取决于当前数据里有没有公式所需的字段。</div></div>
              {[...builtinGroups.entries()].map(([category, items]) => (
                <div className="help-group" key={category}>
                  <h3>{CATEGORY_LABEL[category] ?? category}<span>{items.length}</span></h3>
                  <div className="help-factor-list">
                    {items.map((factor) => (
                      <details key={factor.name} className="help-factor">
                        <summary>
                          <code>{factor.name}</code>
                          <span>{factor.description}</span>
                          <i>展开公式</i>
                        </summary>
                        <div className="help-factor-body">
                          <pre>{factor.expr}</pre>
                          <CopyButton text={factor.expr} label="复制表达式" />
                        </div>
                      </details>
                    ))}
                  </div>
                </div>
              ))}
            </section>
          )}

          {show.faq && (
            <section id="faq" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>常见问题</h2>
                  <p>功能用不了、公式报错、结果看不懂时，先在这里找答案。点击问题展开。</p>
                </div>
                <span className="help-count">{filtered.faqs.length} / {FAQS.length}</span>
              </div>
              {FAQ_GROUPS.map((group) => {
                const items = filtered.faqs.filter((f) => f.group === group)
                if (!items.length) return null
                return (
                  <div className="help-group" key={group}>
                    <h3>{group}<span>{items.length}</span></h3>
                    <div className="help-faq-list">
                      {items.map((f) => (
                        <details key={f.id} id={`faq-${f.id}`} className="help-faq">
                          <summary>{f.q}</summary>
                          <div className="help-guide-body help-faq-body">
                            <HelpMarkdown source={f.body} />
                          </div>
                        </details>
                      ))}
                    </div>
                  </div>
                )
              })}
              {filtered.errors.length > 0 && (
                <div className="help-group" id="faq-errors">
                  <h3>报错信息对照表<span>{filtered.errors.length}</span></h3>
                  <div className="tbl-wrap">
                    <table className="tbl help-err-tbl">
                      <thead><tr><th>编辑器里的报错</th><th>意思</th><th>怎么改</th></tr></thead>
                      <tbody>
                        {filtered.errors.map((e) => (
                          <tr key={e.msg}>
                            <td><code className="inline">{e.msg}</code></td>
                            <td><HelpMarkdown source={e.meaning} /></td>
                            <td><HelpMarkdown source={e.fix} /></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </section>
          )}
        </main>
      </div>
    </HelpNavContext.Provider>
  )
}
