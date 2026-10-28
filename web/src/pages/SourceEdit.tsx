import { useRef, useState } from 'react'
import { ErrorNotice, Loading, Spinner } from '../components/common'
import { JsonTree } from '../components/JsonTree'
import { post } from '../lib/api'
import { ago, useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm } from '../lib/router'
import type { Source, SourceData } from '../lib/types'

type Param = { k: string; v: string; secret?: boolean }
type Preset = {
  name: string
  desc: string
  badge: string
  method: string
  url: string
  params?: Param[]
  body?: string
  headers?: Param[]
  auth_hint?: string
  interval?: number
}
type Data = {
  Source: Source
  HasAuth: boolean
  HeaderRows: { K: string; V: string }[] | null
  SecretKeys: string[] | null
  StaticData: string
  Presets: Preset[]
  PushURL: string
  Data: SourceData
  Intervals: { Sec: number; Label: string }[]
}

export function SourceEdit({ id }: { id: string }) {
  const { data, error, stamp } = useData<Data>(id ? `/api/sources/${id}` : '/api/sources/new')
  useTitle(id ? data?.Source.Name : 'New data source')
  if (!data) return <Loading error={error} />
  return <Editor key={stamp} data={data} />
}

// Query parameters live in the URL, except secret ones: those are posted
// separately (sk/sv), stored encrypted and added when fetching.
type Row = { key: number; k: string; v: string; secret: boolean; saved?: boolean }

let nextKey = 1
const row = (k = '', v = '', secret = false, saved = false): Row => ({ key: nextKey++, k, v, secret, saved })

function splitURL(u: string): [string, string] {
  const i = u.indexOf('?')
  return i < 0 ? [u, ''] : [u.slice(0, i), u.slice(i + 1)]
}
const dec = (s: string) => {
  try {
    return decodeURIComponent(s.replace(/\+/g, ' '))
  } catch {
    return s
  }
}
const enc = (s: string) => encodeURIComponent(s).replace(/%2C/gi, ',').replace(/%3A/gi, ':').replace(/%2F/gi, '/')

function urlRows(u: string): Row[] {
  return splitURL(u)[1]
    .split('&')
    .filter(Boolean)
    .map((pair) => {
      const [k, ...rest] = pair.split('=')
      return row(dec(k), dec(rest.join('=')))
    })
}

function rowsToURL(u: string, rows: Row[]): string {
  const parts = rows.filter((r) => !r.secret && r.k.trim()).map((r) => enc(r.k.trim()) + (r.v !== '' ? '=' + enc(r.v) : ''))
  return parts.length ? splitURL(u)[0] + '?' + parts.join('&') : splitURL(u)[0]
}

function Editor({ data }: { data: Data }) {
  const s = data.Source
  const form = useRef<HTMLFormElement>(null)
  const urlInput = useRef<HTMLInputElement>(null)
  const [name, setName] = useState(s.Name)
  const [kind, setKind] = useState(s.Kind)
  const [method, setMethod] = useState(s.Method || 'GET')
  const [url, setURL] = useState(s.URL)
  const [rows, setRows] = useState<Row[]>(() => [...urlRows(s.URL), ...(data.SecretKeys ?? []).map((k) => row(k, '', true, true))])
  const [headers, setHeaders] = useState<Row[]>(() => (data.HeaderRows ?? []).map((h) => row(h.K, h.V)))
  const [body, setBody] = useState(s.Body)
  const [intervalSec, setIntervalSec] = useState(s.IntervalSec)
  const [authHint, setAuthHint] = useState(data.HasAuth ? '•••••• saved' : 'Bearer token (optional)')
  const [preset, setPreset] = useState(-1)
  const { error, busy, submit } = useForm()

  function editURL(u: string) {
    setURL(u)
    setRows((rs) => [...urlRows(u), ...rs.filter((r) => r.secret)])
  }
  function editRows(next: Row[]) {
    setRows(next)
    setURL((u) => rowsToURL(u, next))
  }
  function applyPreset(i: number) {
    const p = data.Presets[i]
    setPreset(i)
    setKind('http')
    if (!name || preset >= 0) setName(p.name === 'Blank request' ? '' : p.name)
    setMethod(p.method || 'GET')
    setURL(p.url || '')
    setBody(p.body || '')
    if (p.interval) setIntervalSec(p.interval)
    const secrets = (p.params ?? []).filter((x) => x.secret).map((x) => row(x.k, x.v, true))
    setRows([...urlRows(p.url || ''), ...secrets])
    setHeaders((p.headers ?? []).map((h) => row(h.k, h.v)))
    setAuthHint(p.auth_hint || 'none')
    form.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    // Focus a secret value that still needs filling in, else the URL.
    setTimeout(() => {
      const empty = form.current?.querySelector<HTMLInputElement>('.kv-row.secret input[name=sv]')
      ;(empty ?? urlInput.current)?.focus()
    })
  }

  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/sources">Data sources</Link> /
          </p>
          <h1>{s.ID ? s.Name : 'New data source'}</h1>
        </div>
        {s.ID && (
          <PostButton action={`/api/sources/${s.ID}/delete`} className="btn danger" confirm="Delete this source? Screens using it will show empty values.">
            Delete
          </PostButton>
        )}
      </div>
      <ErrorNotice error={error} />

      <div className="grid two editor">
        <form ref={form} onSubmit={submit(s.ID ? `/api/sources/${s.ID}` : '/api/sources')} className="card stack" id="source-form">
          <label>
            Name
            <input name="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Kozhikode weather" required />
          </label>

          <fieldset className="kinds">
            <legend>Type</legend>
            {(
              [
                ['http', 'Request'],
                ['static', 'Fixed data'],
                ['push', 'Webhook'],
              ] as const
            ).map(([k, label]) => (
              <label key={k}>
                <input type="radio" name="kind" value={k} checked={kind === k} onChange={() => setKind(k)} />
                <b>{label}</b>
              </label>
            ))}
          </fieldset>

          <div className="stack" hidden={kind !== 'http'}>
            <div className="url-bar">
              <select name="method" value={method} onChange={(e) => setMethod(e.target.value)}>
                {['GET', 'POST', 'PUT', 'PATCH'].map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
              <input
                ref={urlInput}
                name="url"
                value={url}
                onChange={(e) => editURL(e.target.value)}
                placeholder="https://api.example.com/v1/stats?city=london"
                spellCheck={false}
                autoComplete="off"
              />
            </div>

            <div className="kv-block">
              <div className="kv-head">
                <span className="label-like">Query parameters</span>
              </div>
              <div className="kv-rows">
                {rows.map((r, i) => (
                  <div className={r.secret ? 'kv-row secret' : 'kv-row'} key={r.key}>
                    <input
                      name={r.secret ? 'sk' : undefined}
                      value={r.k}
                      placeholder="name"
                      className="k"
                      onChange={(e) => editRows(rows.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))}
                    />
                    <input
                      name={r.secret ? 'sv' : undefined}
                      type={r.secret ? 'password' : 'text'}
                      value={r.v}
                      placeholder={r.secret ? (r.saved ? '•••••• saved' : 'secret value') : 'value'}
                      className="v"
                      autoComplete="off"
                      onChange={(e) => editRows(rows.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))}
                    />
                    <button
                      type="button"
                      className={r.secret ? 'kv-lock on' : 'kv-lock'}
                      title={r.secret ? 'Secret: encrypted, not in the URL' : 'Make secret'}
                      onClick={() => {
                        const flipped = { ...r, secret: !r.secret }
                        const rest = rows.filter((_, j) => j !== i)
                        editRows(flipped.secret ? [...rest, flipped] : rows.map((x, j) => (j === i ? flipped : x)))
                      }}
                    >
                      🔒
                    </button>
                    <button type="button" className="kv-del" title="Remove" onClick={() => editRows(rows.filter((_, j) => j !== i))}>
                      ×
                    </button>
                  </div>
                ))}
              </div>
              <button type="button" className="kv-add" onClick={() => setRows([...rows, row()])}>
                + Add parameter
              </button>
            </div>

            <label>
              Token
              <input
                name="auth"
                type="password"
                autoComplete="off"
                placeholder={authHint}
                title="Sent as Authorization: Bearer <token>. A value with a space (e.g. Basic …) is sent as-is."
              />
            </label>
            {data.HasAuth && (
              <label className="check small">
                <input type="checkbox" name="clear_auth" value="1" /> Remove the saved token
              </label>
            )}

            <div className="kv-block">
              <div className="kv-head">
                <span className="label-like">Headers</span>
              </div>
              <div className="kv-rows">
                {headers.map((h, i) => (
                  <div className="kv-row" key={h.key}>
                    <input
                      name="hk"
                      value={h.k}
                      placeholder="Header-Name"
                      className="k"
                      onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))}
                    />
                    <input
                      name="hv"
                      value={h.v}
                      placeholder="value"
                      className="v"
                      onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))}
                    />
                    <button type="button" className="kv-del" title="Remove" onClick={() => setHeaders(headers.filter((_, j) => j !== i))}>
                      ×
                    </button>
                  </div>
                ))}
              </div>
              <button type="button" className="kv-add" onClick={() => setHeaders([...headers, row()])}>
                + Add header
              </button>
            </div>

            {/* Body only matters for methods that send one. */}
            <label hidden={method === 'GET'}>
              JSON body
              <textarea
                name="body"
                rows={5}
                className="mono"
                placeholder='{"query": "..."}'
                spellCheck={false}
                value={body}
                onChange={(e) => setBody(e.target.value)}
              />
            </label>

            <label>
              Refresh every
              <select name="interval_sec" value={intervalSec} onChange={(e) => setIntervalSec(Number(e.target.value))}>
                {data.Intervals.map((o) => (
                  <option key={o.Sec} value={o.Sec}>
                    {o.Label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className="stack" hidden={kind !== 'static'}>
            <label>
              Data
              <textarea
                name="static_data"
                rows={10}
                className="mono"
                placeholder='{"note": "Ship it!", "goal": 72}'
                spellCheck={false}
                defaultValue={data.StaticData}
              />
            </label>
          </div>

          <div className="stack" hidden={kind !== 'push'}>
            <p className="muted small">POST JSON to this URL to update the data.</p>
            <label className="check" title="For data about a person, like their usage or steps, not shared data like a price.">
              <input type="checkbox" name="per_user" value="1" defaultChecked={s.PerUser} />
              Personal: when you publish an app using it, everyone who installs the app sends their own data
            </label>
            <label>
              Example data <span className="muted small">personal sources: what App Store previews show people before they send their own</span>
              <textarea name="example" rows={4} className="mono" spellCheck={false} defaultValue={s.Example} placeholder='{"session": {"used": 37}}' />
            </label>
            {s.ID && s.PushToken ? (
              <>
                <label>
                  Webhook URL
                  <input readOnly value={data.PushURL} className="mono" onClick={(e) => e.currentTarget.select()} />
                </label>
                <pre className="json">{`curl -X POST ${data.PushURL} \\
  -H 'Content-Type: application/json' \\
  -d '{"value": 42, "label": "Deploys"}'`}</pre>
              </>
            ) : (
              <p className="muted small">Save to get your private webhook URL.</p>
            )}
          </div>

          <div className="savebar">
            <button className="btn btn-lg" disabled={busy}>
              {busy ? <Spinner /> : null} {s.ID ? 'Save' : 'Save & fetch'}
            </button>
          </div>
        </form>
        <aside className="stack sticky">
          {s.ID ? (
            <Response initial={data.Data} />
          ) : (
            <section className="card templates">
              <h3>Start from a template</h3>
              <div className="template-list">
                {data.Presets.map((p, i) => (
                  <button type="button" className={preset === i ? 'template-card on' : 'template-card'} key={i} onClick={() => applyPreset(i)}>
                    <span className="tpl-badge">{p.badge}</span>
                    <span>
                      <strong>{p.name}</strong>
                      <span className="muted small">{p.desc}</span>
                    </span>
                  </button>
                ))}
              </div>
            </section>
          )}
        </aside>
      </div>
    </>
  )
}

function readCollapsed() {
  try {
    return localStorage.getItem('resp-collapsed') === '1'
  } catch {
    return false
  }
}

// Response is the source's latest data, with Fetch now.
function Response({ initial }: { initial: SourceData }) {
  const [d, setD] = useState(initial)
  const [tab, setTab] = useState<'tree' | 'raw'>('tree')
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [fetching, setFetching] = useState(false)
  const src = d.Source
  async function fetchNow() {
    setFetching(true)
    try {
      setD(await post<SourceData>(`/api/sources/${src.ID}/fetch`))
    } finally {
      setFetching(false)
    }
  }
  function toggle() {
    const c = !collapsed
    setCollapsed(c)
    try {
      localStorage.setItem('resp-collapsed', c ? '1' : '')
    } catch {
      // private mode: just don't remember
    }
  }
  return (
    <section className={collapsed ? 'card resp collapsed' : 'card resp'}>
      <div className="resp-head">
        <div className="resp-title">
          <button type="button" className="resp-toggle" aria-label="Collapse response" onClick={toggle}>
            ▾
          </button>
          <h3>Response</h3>
          {src.LastError ? (
            <span className="pill err">Error{src.LastStatus ? ` ${src.LastStatus}` : ''}</span>
          ) : src.LastStatus ? (
            <span className="pill ok">{src.LastStatus} OK</span>
          ) : null}
          <span className="muted small">updated {ago(src.LastFetchedAt)}</span>
          {fetching && (
            <span className="fetching small muted">
              <Spinner /> fetching…
            </span>
          )}
        </div>
        <div className="row">
          {src.Kind === 'http' && (
            <button type="button" className="btn ghost small" onClick={fetchNow} disabled={fetching}>
              Fetch now
            </button>
          )}
          {d.Pretty && <Link className="btn small" to={`/screens/new?source=${src.ID}`}>Make a screen →</Link>}
        </div>
      </div>
      <div className="resp-body">
        <ErrorNotice error={src.LastError} />
        {d.Pretty ? (
          <>
            <div className="tabs">
              <button type="button" className={tab === 'tree' ? 'tab on' : 'tab'} onClick={() => setTab('tree')}>
                Tree
              </button>
              <button type="button" className={tab === 'raw' ? 'tab on' : 'tab'} onClick={() => setTab('raw')}>
                Raw
              </button>
            </div>
            {tab === 'tree' ? (
              <div className="tab-pane">
                <JsonTree key={src.LastData} json={src.LastData} mode="copy" />
              </div>
            ) : (
              <div className="tab-pane">
                <pre className="json">{d.Pretty}</pre>
              </div>
            )}
          </>
        ) : (
          <p className="muted">
            No data yet.
            {src.Kind === 'http' && (
              <>
                {' '}
                Press <b>Fetch now</b>.
              </>
            )}
          </p>
        )}
      </div>
    </section>
  )
}
