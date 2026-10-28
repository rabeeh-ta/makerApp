import { useState } from 'react'
import { Loading } from '../components/common'
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

const json = (v: unknown) => JSON.stringify(v, null, 2)

// How each AI app adds makerApp, given the connector URL. None needs a key:
// the first time it connects, it opens makerApp's sign-in and Allow page.
const agents: { id: string; label: string; how: string; config: (url: string) => string }[] = [
  {
    id: 'claude-code',
    label: 'Claude Code',
    how: 'Run this once on your computer, then run /mcp in Claude Code and choose makerapp to sign in. It works in every project there.',
    config: (url) => `claude mcp add --scope user --transport http makerapp ${url}`,
  },
  {
    id: 'claude',
    label: 'Claude app',
    how: 'In Claude on the web, desktop or phone: Settings → Connectors → Add custom connector. Name it makerApp, paste this URL, then Connect, sign in here and press Allow.',
    config: (url) => url,
  },
  {
    id: 'codex',
    label: 'Codex',
    how: 'Run this once; Codex opens the sign-in by itself. To sign in again: codex mcp login makerapp.',
    config: (url) => `codex mcp add makerapp --url ${url}`,
  },
  {
    id: 'cursor',
    label: 'Cursor',
    how: 'Put this in ~/.cursor/mcp.json, or add the makerapp entry to the servers already there. Cursor asks you to sign in the first time.',
    config: (url) => json({ mcpServers: { makerapp: { url } } }),
  },
  {
    id: 'vscode',
    label: 'VS Code',
    how: 'Run this once; it adds makerApp to your VS Code profile for agent mode and asks you to sign in when it first connects.',
    config: (url) => `code --add-mcp '${JSON.stringify({ name: 'makerapp', type: 'http', url })}'`,
  },
  {
    id: 'gemini',
    label: 'Gemini CLI',
    how: 'Run this once, then /mcp auth makerapp in Gemini CLI to sign in.',
    config: (url) => `gemini mcp add --scope user --transport http makerapp ${url}`,
  },
  {
    id: 'other',
    label: 'Other',
    how: 'Any app that supports remote MCP servers connects to this URL and signs in through makerApp. Most take JSON like this.',
    config: (url) => json({ mcpServers: { makerapp: { type: 'http', url } } }),
  },
]

const agentKey = 'makerapp:mcp-agent'

function remembered(): string | null {
  try {
    return localStorage.getItem(agentKey)
  } catch {
    return null
  }
}

const copy = (text: string) => navigator.clipboard?.writeText(text).then(() => toast('Copied'), () => toast(text))

// Connect is the MCP page: connect Claude, Codex or another AI app to
// makerApp, what to ask it, and the apps connected now.
export function Connect() {
  useTitle('MCP')
  const { data, error } = useData<{ Grants: Grant[] | null; URL: string }>('/api/oauth/grants')
  const [chosen, setChosen] = useState(remembered)
  if (!data) return <Loading error={error} />
  const grants = data.Grants ?? []
  const agent = agents.find((a) => a.id === chosen) ?? agents[0]
  const config = agent.config(data.URL)
  function choose(id: string) {
    setChosen(id)
    try {
      localStorage.setItem(agentKey, id)
    } catch {
      // Private windows: the choice just isn't remembered.
    }
  }
  return (
    <>
      <div className="page-head">
        <div>
          <h1>MCP</h1>
          <p className="muted connect-lead">
            Connect Claude, Codex or another AI app to makerApp, describe the app you want, and it builds it here: data sources, apps and
            screens. It can install it on your board too.
          </p>
        </div>
      </div>

      <section className="card stack">
        <fieldset className="seg chips">
          <legend>Your AI app</legend>
          {agents.map((a) => (
            <label key={a.id}>
              <input type="radio" name="agent" checked={agent === a} onChange={() => choose(a.id)} />
              {a.label}
            </label>
          ))}
        </fieldset>
        <p className="muted small">{agent.how}</p>
        <div className="command">
          <pre className="json">{config}</pre>
          <button type="button" className="btn ghost small" onClick={() => copy(config)}>
            Copy
          </button>
        </div>
        <p className="muted small">No key to copy: makerApp asks you to sign in and press Allow, and you can disconnect it below.</p>
      </section>

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
