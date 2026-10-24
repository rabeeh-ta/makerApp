import { Byline, ErrorNotice, Loading, Notice, OnlineBadge } from '../components/common'
import { DeviceScreen } from '../components/DeviceScreen'
import { cycleName, layoutName, useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm, useLocation } from '../lib/router'
import type { App, Device, Screen } from '../lib/types'

type Data = {
  App: App
  Screens: { S: Screen; First: boolean; Last: boolean }[] | null
  Devices: (Device & { Installed: boolean; Online: boolean })[] | null
  CycleOpts: number[]
  Username: string
  Installs: number
  Sources: string[] | null
}

const savedNotes: Record<string, string> = {
  '1': 'Saved. Boards with this app have the update.',
  added: 'Screen added.',
  published: 'Published. Anyone can find it under All apps and install it.',
  unpublished: "Unpublished. It's off the list and off other people's boards.",
}

export function AppPage({ id }: { id: string }) {
  const { query } = useLocation()
  const { data, error, stamp } = useData<Data>(`/api/apps/${id}`)
  useTitle(data?.App.Name)
  if (!data) return <Loading error={error} />
  const app = data.App
  const screens = data.Screens ?? []
  const devices = data.Devices ?? []
  const saved = savedNotes[query.get('saved') ?? '']
  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/apps">Apps</Link> /
          </p>
          <h1>{app.Name}</h1>
          {app.MadeBy && <span className="pill made-by">Made with {app.MadeBy}</span>}
        </div>
        <PostButton action={`/api/apps/${app.ID}/delete`} className="btn danger" confirm={`Delete “${app.Name}” and its screens? Boards with it installed lose it.`}>
          Delete app
        </PostButton>
      </div>
      {saved && <Notice>{saved}</Notice>}

      <div className="grid two wide-left">
        <section>
          <div className="row between section-head">
            <span className="muted small">
              On the board, the top and bottom buttons move between these screens. The middle button goes back to the Apps drawer.
            </span>
            <Link className="btn small" to={`/screens/new?app=${app.ID}`}>New screen</Link>
          </div>
          {screens.length ? (
            <div className="grid cards">
              {screens.map(({ S, First, Last }) => (
                <div className="card screen-card" key={S.id}>
                  <Link className="screen-link" to={`/screens/${S.id}`}>
                    <div className="device-thumb">
                      <DeviceScreen s={S} />
                    </div>
                    <div className="row between">
                      <strong>{S.name}</strong>
                      <span className="muted small">{layoutName(S.layout)}</span>
                    </div>
                  </Link>
                  <div className="card-actions">
                    <PostButton action={`/api/apps/${app.ID}/screens/${S.id}/move`} data={{ dir: 'up' }} className="btn ghost small icon" disabled={First} title="Move earlier">
                      ↑
                    </PostButton>
                    <PostButton action={`/api/apps/${app.ID}/screens/${S.id}/move`} data={{ dir: 'down' }} className="btn ghost small icon" disabled={Last} title="Move later">
                      ↓
                    </PostButton>
                    <span className="grow" />
                    <Link className="btn ghost small" to={`/screens/${S.id}`}>Edit</Link>
                    <PostButton action={`/api/screens/${S.id}/delete`} className="btn danger small" confirm={`Delete “${S.name}”?`}>
                      Delete
                    </PostButton>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="card empty">
              <h3>No screens yet</h3>
              <p className="muted">Add a screen to show data from a source.</p>
              <Link className="btn" to={`/screens/new?app=${app.ID}`}>New screen</Link>
            </div>
          )}
        </section>

        <aside className="stack">
          <Settings key={`s${stamp}`} data={data} />
          <Publish key={`p${stamp}`} data={data} />
          <section className="card">
            <h3>Installed on</h3>
            {devices.length ? (
              devices.map((d) => (
                <div className="install-row" key={d.ID}>
                  <span className="grow">
                    <strong>{d.Name}</strong> <OnlineBadge online={d.Online} />
                  </span>
                  {d.Installed ? (
                    <PostButton action={`/api/apps/${app.ID}/uninstall`} data={{ device: d.ID, back: `/apps/${app.ID}` }} className="btn ghost small">
                      Uninstall
                    </PostButton>
                  ) : (
                    <PostButton action={`/api/apps/${app.ID}/install`} data={{ device: d.ID, back: `/apps/${app.ID}` }}>
                      Install
                    </PostButton>
                  )}
                </div>
              ))
            ) : (
              <p className="muted small">
                No boards yet. <Link to="/flash">Add one</Link>.
              </p>
            )}
          </section>
        </aside>
      </div>
    </>
  )
}

function Settings({ data }: { data: Data }) {
  const app = data.App
  const { error, busy, submit } = useForm()
  return (
    <section className="card">
      <h3>App</h3>
      <ErrorNotice error={error} />
      <form onSubmit={submit(`/api/apps/${app.ID}`)} className="stack">
        <label>
          Name
          <input name="name" defaultValue={app.Name} maxLength={40} required />
        </label>
        <label title="Turns to the next screen when nobody has pressed a button for this long. Needs 2 or more screens.">
          Cycle screens
          <select name="cycle_sec" defaultValue={app.CycleSec}>
            {data.CycleOpts.map((s) => (
              <option key={s} value={s}>
                {cycleName(s)}
              </option>
            ))}
          </select>
        </label>
        <button className="btn" disabled={busy}>Save</button>
      </form>
    </section>
  )
}

function Publish({ data }: { data: Data }) {
  const app = data.App
  const { error, busy, submit } = useForm()
  const sources = data.Sources ?? []
  if (app.PublishedAt) {
    return (
      <section className="card">
        <h3>Publish</h3>
        <p className="small row public-row">
          Public as <Byline by={data.Username} installs={data.Installs} />
        </p>
        <p className="muted small">{app.Description}</p>
        <p className="muted small">Changes you make reach everyone's boards right away.</p>
        <PostButton action={`/api/apps/${app.ID}/unpublish`} className="btn ghost small" confirm={`Unpublish “${app.Name}”? It comes off the list and off other people's boards.`}>
          Unpublish
        </PostButton>
      </section>
    )
  }
  return (
    <section className="card">
      <h3>Publish</h3>
      <ErrorNotice error={error} />
      <form onSubmit={submit(`/api/apps/${app.ID}/publish`)} className="stack">
        <p className="muted small">Anyone can find it under All apps and install it, straight away.</p>
        {!data.Username && (
          <label>
            Your public name
            <input name="username" required pattern="[a-zA-Z0-9_\-]{3,20}" maxLength={20} placeholder="e.g. rabeeh" autoComplete="username" />
          </label>
        )}
        <label>
          Short description
          <input name="description" required maxLength={140} defaultValue={app.Description} placeholder="What it shows" />
        </label>
        {sources.length > 0 && (
          <p className="warn small">
            Everyone who installs it sees the data from {sources.map((s) => `“${s}”`).join(', ')}, fetched with your account.
          </p>
        )}
        <button className="btn small" disabled={busy}>Publish{data.Username && ` as @${data.Username}`}</button>
      </form>
    </section>
  )
}
