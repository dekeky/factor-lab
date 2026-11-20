import startSource from './docs/start.md?raw'
import syntaxSource from './docs/syntax.md?raw'
import glossarySource from './docs/glossary.md?raw'
import faqSource from './docs/faq.md?raw'
import errorsSource from './docs/errors.md?raw'

export type GuideLevel = '入门' | '进阶'

export interface GuideDoc {
  id: string
  level: GuideLevel
  minutes: number
  title: string
  summary: string
  keywords: string
  body: string
}

export interface FaqDoc {
  id: string
  group: string
  q: string
  keywords: string
  body: string
}

export interface TermDoc {
  group: string
  term: string
  alias: string
  body: string
}

export interface ErrorHint {
  msg: string
  meaning: string
  fix: string
}

function parseFrontmatter(raw: string): { data: Record<string, string>; body: string } {
  const src = raw.replace(/^\uFEFF/, '')
  const m = src.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/)
  if (!m) return { data: {}, body: src }
  const data: Record<string, string> = {}
  for (const line of m[1].split(/\r?\n/)) {
    const i = line.indexOf(':')
    if (i <= 0) continue
    let value = line.slice(i + 1).trim()
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1)
    }
    data[line.slice(0, i).trim()] = value
  }
  return { data, body: m[2].replace(/^\r?\n/, '') }
}

function splitByHeading(src: string, marker: '## ' | '### '): { title: string; body: string }[] {
  return src
    .split(new RegExp(`^${marker}`, 'm'))
    .slice(1)
    .map((chunk) => {
      const i = chunk.search(/\r?\n/)
      return {
        title: (i < 0 ? chunk : chunk.slice(0, i)).trim(),
        body: i < 0 ? '' : chunk.slice(i + 1).trim(),
      }
    })
}

const guideFiles = import.meta.glob('./docs/guides/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

export const START_MD = startSource
export const SYNTAX_MD = syntaxSource

export const GUIDES: GuideDoc[] = Object.entries(guideFiles)
  .sort(([a], [b]) => a.localeCompare(b, 'en'))
  .map(([path, raw]) => {
    const { data, body } = parseFrontmatter(raw)
    const fallbackId = path.replace(/^.*[/\\]/, '').replace(/^\d+-/, '').replace(/\.md$/, '')
    return {
      id: data.id || fallbackId,
      level: data.level === '进阶' ? '进阶' : '入门',
      minutes: Number(data.minutes || 0),
      title: data.title || '',
      summary: data.summary || '',
      keywords: data.keywords || '',
      body,
    }
  })

export const FAQ_GROUPS = ['准备工作', '写因子', '看结果', '因子库与组合', '实盘模拟'] as const

export const FAQS: FaqDoc[] = splitByHeading(faqSource, '## ').flatMap(({ title: group, body }) =>
  splitByHeading(body, '### ').map(({ title, body: item }) => {
    const [id, ...qParts] = title.split(/\s*\|\s*/)
    const lines = item.split(/\r?\n/)
    let keywords = ''
    let start = 0
    if (lines[0]?.startsWith('keywords:')) {
      keywords = lines[0].slice('keywords:'.length).trim()
      start = lines[1] === '' ? 2 : 1
    }
    return {
      id: id.trim(),
      group,
      q: (qParts.join(' | ') || id).trim(),
      keywords,
      body: lines.slice(start).join('\n').trim(),
    }
  }),
)

export const GLOSSARY_GROUPS = ['基础概念', '评价指标', '回测与交易', '数据口径'] as const

export const GLOSSARY: TermDoc[] = splitByHeading(glossarySource, '## ').flatMap(({ title: group, body }) =>
  splitByHeading(body, '### ').map(({ title, body: item }) => {
    const [term, alias = ''] = title.split(/\s*\|\s*/)
    return { group, term: term.trim(), alias: alias.trim(), body: item.trim() }
  }),
)

export const ERROR_HINTS: ErrorHint[] = errorsSource
  .split(/\r?\n/)
  .filter((line) => line.startsWith('|') && !/^\|\s*-/.test(line))
  .slice(1)
  .map((line) => {
    const cells = line
      .split('|')
      .slice(1, -1)
      .map((cell) => cell.trim().replace(/^`|`$/g, ''))
    return { msg: cells[0] || '', meaning: cells[1] || '', fix: cells[2] || '' }
  })
  .filter((row) => row.msg)
