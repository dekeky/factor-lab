import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { CATEGORY_LABEL } from '../components/Layout'
import {
  BUILTINS_MD,
  ERROR_HINTS,
  FAQS,
  FAQ_GROUPS,
  FIELDS_MD,
  GUIDES,
  SYNTAX_MD,
} from '../help/catalog'
import { CopyButton, HelpMarkdown, HelpNavContext, interpolate } from '../help/Markdown'
import type { HelpDoc } from '../types'

const sections = [
  ['research', '因子研究'],
  ['syntax', 'DSL 语法'],
  ['operators', '算子速查'],
  ['fields', '基础指标'],
  ['builtins', '内置因子'],
  ['faq', '附录'],
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
    const fieldsText = interpolate(FIELDS_MD, { field_count: String(doc.fields.length) })
    const builtinsText = interpolate(BUILTINS_MD, { builtin_count: String(doc.builtins.length) })
    return {
      chapters: GUIDES.filter((x) => includes(q, x.title, x.summary, x.level, x.keywords, x.body)),
      fields: doc.fields.filter((x) => includes(q, x.name, x.description, x.unit, x.tip, fieldsText)),
      operators: doc.operators.filter((x) => includes(q, x.name, x.category, x.signature, x.description, x.example, x.tip)),
      builtins: doc.builtins.filter((x) => includes(q, x.name, x.category, x.description, x.expr, CATEGORY_LABEL[x.category], builtinsText)),
      faqs: FAQS.filter((x) => includes(q, x.q, x.keywords, x.body)),
      errors: ERROR_HINTS.filter((x) => includes(q, x.msg, x.meaning, x.fix)),
      syntax: includes(q, syntaxText),
      fieldsIntro: includes(q, fieldsText),
      builtinsIntro: includes(q, builtinsText),
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

  if (error) return <div className="note err">文档加载失败：{error}</div>
  if (!doc || !filtered) return <div className="busy-line"><span className="spinner" />正在读取当前面板字段与算子…</div>

  const operatorGroups = [...groupBy(filtered.operators, (item) => item.category).entries()].sort(
    ([a], [b]) => operatorRank(a) - operatorRank(b),
  )
  const builtinGroups = groupBy(filtered.builtins, (item) => item.category)

  const show = {
    research: filtered.chapters.length > 0,
    syntax: filtered.syntax || filtered.operators.length > 0,
    operators: filtered.operators.length > 0,
    fields: !q || filtered.fields.length > 0 || filtered.fieldsIntro,
    builtins: filtered.builtins.length > 0 || filtered.builtinsIntro,
    faq: filtered.faqs.length > 0 || filtered.errors.length > 0,
  }
  const hits =
    filtered.chapters.length + filtered.fields.length + filtered.operators.length +
    filtered.builtins.length + filtered.faqs.length + filtered.errors.length +
    (filtered.syntax ? 1 : 0)
  const nothing = !!q && hits === 0

  return (
    <HelpNavContext.Provider value={{ openGuide, jump }}>
      <div className="help-page">
        <aside className="help-toc" aria-label="文档目录">
          <div className="help-toc-title">目录</div>
          {sections.map(([id, label]) => (
            <button key={id} onClick={() => jump(id)}>{label}</button>
          ))}
          <div className="help-toc-meta">
            <b>{GUIDES.length}</b> 章教材 · <b>{doc.fields.length}</b> 个字段<br />
            <b>{doc.operators.length}</b> 个算子 · <b>{doc.builtins.length}</b> 个内置因子
          </div>
        </aside>

        <main className="help-doc">
          <div className="help-hero">
            <div>
              <h1>文档</h1>
              <p>
                按顺序读完「因子研究」就能独立写表达式、读评价报告。
                DSL、字段、内置因子另立专章；字段与算子随当前面板更新。
              </p>
            </div>
            <span className="tag green">随当前服务动态更新</span>
          </div>

          <label className="help-search">
            <span>⌕</span>
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索章节、字段、算子、表达式或报错…"
              aria-label="搜索文档"
            />
            {query && <button onClick={() => setQuery('')} aria-label="清空搜索">×</button>}
          </label>

          {q && !nothing && <p className="help-result">找到 <b>{hits}</b> 条与“{query.trim()}”相关的内容。</p>}
          {nothing && (
            <div className="help-empty">
              没有与“{query.trim()}”匹配的条目。可试更短的词，例如 IC、窗口、cs_rank、换手。
            </div>
          )}

          {show.research && (
            <section id="research" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>因子研究</h2>
                  <p>从定义、面板、时间对齐讲到评价、组合与模拟。表达式写法见下一章 DSL 语法。</p>
                </div>
                <span className="help-count">{GUIDES.reduce((n, g) => n + g.minutes, 0)} 分钟</span>
              </div>
              <div className="help-guide-list">
                {filtered.chapters.map((guide) => (
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

          {show.syntax && (
            <section id="syntax" className="help-section">
              <h2>DSL 语法</h2>
              <HelpMarkdown source={SYNTAX_MD} vars={syntaxVars} />
            </section>
          )}

          {show.operators && (
            <section id="operators" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>算子速查</h2>
                  <p>
                    由当前计算引擎注册表生成。<code>ts_</code> 按股票沿时间滚动，<code>cs_</code> 按交易日做横截面，其余为逐元素变换。窗口必须是 1～{doc.dsl.max_window} 的整数常量。
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

          {show.fields && (
            <section id="fields" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>基础指标</h2>
                </div>
                <span className="help-count">{filtered.fields.length} / {doc.fields.length}</span>
              </div>
              <HelpMarkdown
                source={FIELDS_MD}
                vars={{ field_count: String(doc.fields.length) }}
              />
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
                <div className="help-empty">当前面板没有可引用字段。在「设置」中填写 Tushare Token，或改用带数值列的 CSV。</div>
              )}
            </section>
          )}

          {show.builtins && (
            <section id="builtins" className="help-section">
              <div className="help-section-head">
                <div>
                  <h2>内置因子</h2>
                </div>
                <span className="help-count">{filtered.builtins.length} / {doc.builtins.length}</span>
              </div>
              <HelpMarkdown
                source={BUILTINS_MD}
                vars={{ builtin_count: String(doc.builtins.length) }}
              />
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
                  <h2>附录</h2>
                  <p>环境配置、报错对照与操作性问题。概念与口径以正文为准。</p>
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
                  <h3>报错对照<span>{filtered.errors.length}</span></h3>
                  <div className="tbl-wrap">
                    <table className="tbl help-err-tbl">
                      <thead><tr><th>校验信息</th><th>原因</th><th>处理</th></tr></thead>
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
