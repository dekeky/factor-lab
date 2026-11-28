import { Streamdown, type ControlsConfig, type StreamdownTranslations } from 'streamdown'

const TRANSLATIONS: Partial<StreamdownTranslations> = {
  copyCode: '复制代码',
  copyTable: '复制表格',
  copyTableAsMarkdown: '复制为 Markdown',
  copyTableAsCsv: '复制为 CSV',
  copyTableAsTsv: '复制为 TSV',
  copied: '已复制',
  copyLink: '复制链接',
  close: '关闭',
  openLink: '打开链接',
  openExternalLink: '打开外部链接',
  externalLinkWarning: '即将打开外部网站，请确认链接安全。',
}

const CONTROLS: ControlsConfig = {
  code: { download: false },
  table: false,
  mermaid: false,
  image: false,
}

/** AI 助手气泡：Streamdown 渲染流式 Markdown（GFM + 半截语法修补）。 */
export function ChatMarkdown({ source, streaming }: { source: string; streaming?: boolean }) {
  return (
    <Streamdown
      className="ac-md"
      mode={streaming ? 'streaming' : 'static'}
      isAnimating={!!streaming}
      parseIncompleteMarkdown
      dir="auto"
      lineNumbers={false}
      codeBlockMaxHeight={220}
      controls={CONTROLS}
      translations={TRANSLATIONS}
      linkSafety={{ enabled: false }}
    >
      {source}
    </Streamdown>
  )
}
