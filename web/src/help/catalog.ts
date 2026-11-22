import syntaxSource from './docs/syntax.md?raw'
import fieldsSource from './docs/fields.md?raw'
import builtinsSource from './docs/builtins.md?raw'
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

const chapterFiles = import.meta.glob('./docs/textbook/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

export const SYNTAX_MD = syntaxSource
export const FIELDS_MD = fieldsSource
export const BUILTINS_MD = builtinsSource

export const GUIDES: GuideDoc[] = Object.entries(chapterFiles)
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

export const FAQ_GROUPS = ['环境与数据', '表达式', '评价与模拟'] as const

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
