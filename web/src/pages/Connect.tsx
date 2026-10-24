import { DataTabs, Loading } from '../components/common'
import { ago, useTitle } from '../lib/format'
import { PostButton, useData } from '../lib/router'
import { toast } from '../lib/toast'

type Grant = { ID: string; ClientName: string; CreatedAt: number; LastUsedAt: number }

const examples = [
  'Show the Bitcoin price and its 24-hour change on my board.',
  "Make an app with today's weather in Kozhikode and the next few hours.",
  'Add a screen with the stars and open issues of my GitHub repo.',
  'My weather screen is blank. Fix it.',
]

// Connect is the Data section's AI connector tab: how to connect Claude,
// Codex and other MCP apps, and the ones connected now.
export function Connect() {
  useTitle('AI connector')
  const { data, error } = useData<{ Grants: Grant[] | null; URL: string }>('/api/oauth/grants')
  if (!data) return <Loading error={error} />
  const url = data.URL
  const grants = data.Grants ?? []
  const copy = (text: string) => navigator.clipboard?.writeText(text).then(() => toast('Copied'), () => toast(text))
  const command = (text: string) => (
    <div className="command">
      <pre className="json">{text}</pre>
      <button type="button" className="btn ghost small" onClick={() => copy(text)}>
        Copy
      </button>
    </div>
  )
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Data</h1>
        </div>
      </div>
      <DataTabs tab="ai" />
      <p className="muted connect-lead">
        Connect Claude, Codex or another AI app to makerApp over MCP, describe the app you want, and it builds it here: data sources, apps and
        screens. It can install it on your board too.
      </p>

      <section className="card connect-url">
        <h3>Connector URL</h3>
        <div className="row">
          <input readOnly value={url} className="mono grow" onClick={(e) => e.currentTarget.select()} />
          <button type="button" className="btn" onClick={() => copy(url)}>
            Copy
          </button>
        </div>
      </section>

      <div className="grid two connect-steps">
        <section className="card">
          <h3>Claude (web, desktop, mobile)</h3>
          <ol className="mini">
            <li>
              <b>Settings → Connectors → Add custom connector</b>
            </li>
            <li>Name it makerApp and paste the URL</li>
            <li>Connect, log in here and press Allow</li>
          </ol>
        </section>
        <section className="card">
          <h3>Claude Code</h3>
          {command(`claude mcp add --transport http makerapp ${url}`)}
          <p className="muted small">Then run /mcp in Claude Code and choose makerapp to sign in.</p>
        </section>
        <section className="card">
          <h3>Codex</h3>
          {command(`codex mcp add makerapp --url ${url}`)}
          <p className="muted small">Codex opens the sign-in by itself. To sign in again: codex mcp login makerapp</p>
        </section>
        <section className="card">
          <h3>Other AI apps</h3>
          <p className="muted small">Any app that supports MCP connectors: add a connector with the URL above. It signs in the same way.</p>
        </section>
      </div>

      <h3 className="section-title">Then ask</h3>
      <div className="card examples">
        {examples.map((e) => (
          <p key={e}>“{e}”</p>
        ))}
        <p className="muted small">
          When a data source needs an API key, the AI gives you a link to add it here. Keys never go through the chat.
        </p>
      </div>

      <h3 className="section-title">Connected</h3>
      {grants.length ? (
        <div className="card install-list">
          {grants.map((g) => (
            <div className="install-row" key={g.ID}>
              <div className="grow">
                <strong>{g.ClientName}</strong>
                <div className="muted small">
                  Connected {ago(g.CreatedAt)} · used {ago(g.LastUsedAt)}
                </div>
              </div>
              <PostButton
                action={`/api/oauth/grants/${g.ID}/revoke`}
                className="btn danger small"
                confirm={`Disconnect ${g.ClientName}? It stops working right away; connect it again to use it.`}
              >
                Disconnect
              </PostButton>
            </div>
          ))}
        </div>
      ) : (
        <div className="card empty">
          <p className="muted">Nothing connected yet.</p>
        </div>
      )}
    </>
  )
}
