export function AiIcon({ size = '1em' }: { size?: number | string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" style={{ display: 'block' }}>
      <path d="M10 5.5C10.55 10.1 13.9 13.45 18.5 14C13.9 14.55 10.55 17.9 10 22.5C9.45 17.9 6.1 14.55 1.5 14C6.1 13.45 9.45 10.1 10 5.5Z" />
      <path d="M18.5 1.5C18.73 3.6 20.4 5.27 22.5 5.5C20.4 5.73 18.73 7.4 18.5 9.5C18.27 7.4 16.6 5.73 14.5 5.5C16.6 5.27 18.27 3.6 18.5 1.5Z" opacity="0.85" />
    </svg>
  )
}
