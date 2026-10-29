import { lazy, Suspense } from 'react'

// The Markdown renderer is about a third of the site's script, and only README
// pages need it, so it loads when one opens.
const MarkdownView = lazy(() => import('./MarkdownView'))

// Markdown shows an app's README (see MarkdownView).
export function Markdown({ text }: { text: string }) {
  return (
    <Suspense fallback={<p className="muted small">Loading…</p>}>
      <MarkdownView text={text} />
    </Suspense>
  )
}
