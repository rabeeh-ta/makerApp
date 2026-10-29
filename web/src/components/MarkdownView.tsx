import { useRef, type ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { copy } from '../lib/toast'

// MarkdownView shows an app's README: GitHub-flavored Markdown, written by
// its publisher and read by everyone. HTML in it is dropped and unsafe links
// (javascript: and the like) are emptied, so a README can't run anything on
// the page. Links open in a new tab; code blocks get a Copy button.
export default function MarkdownView({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer nofollow" />,
          img: ({ node, ...props }) => <img {...props} loading="lazy" referrerPolicy="no-referrer" />,
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const pre = useRef<HTMLPreElement>(null)
  return (
    <div className="command">
      <pre ref={pre}>{children}</pre>
      <button type="button" className="btn ghost small" onClick={() => copy(pre.current?.textContent?.replace(/\n$/, '') ?? '')}>
        Copy
      </button>
    </div>
  )
}
