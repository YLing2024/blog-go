export function formatDate(value: string | null | undefined): string {
  const date = String(value || '').trim().split(' ')[0]
  return date ? date.replace(/-/g, '.') : ''
}

// Heading text → stable DOM id for anchors and TOC jumps.
// Plain sequence (sec-0, sec-1…), not the title text: CJK titles would be
// percent-encoded into noise, and editing a title would break old deep links.
export function headingId(i: number): string {
  return `sec-${i}`
}
