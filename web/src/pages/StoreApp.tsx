import { Byline, Loading, Notice } from '../components/common'
import { DeviceScreen } from '../components/DeviceScreen'
import { Markdown } from '../components/Markdown'
import { ago, useTitle } from '../lib/format'
import { Link, PostButton, useData } from '../lib/router'
import { copy } from '../lib/toast'
import type { Device, Screen } from '../lib/types'

type Page = {
  ID: string
  Name: string
  Desc: string
  By: string
  Installs: number
  Screens: Screen[] | null
  Devices: (Device & { Installed: boolean; NeedsFW: string })[] | null
  Mine?: boolean
  Public?: boolean
  Setup?: string // the README, with the viewer's push link filled in
  NeedsData?: string[] | null // personal sources, before installing
  Personal?: { Name: string; PushURL: string; LastFetchedAt: number; Data: string }[] | null
}

// StoreApp is an app's page in the App Store: its screens and README,
// installing it on your boards, and setting it up. Apps that show your own
// data give you your own push link here.
export function StoreApp({ id }: { id: string }) {
  const { data, error } = useData<Page>(`/api/store/${id}`)
  useTitle(data?.Name)
  if (!data) return <Loading error={error} />
  const devices = data.Devices ?? []
  const needs = data.NeedsData ?? []
  const personal = data.Personal ?? []
  const back = `/store/${data.ID}`
  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/store">App Store</Link> /
          </p>
          <h1>{data.Name}</h1>
          <p className="row public-row">
            <Byline by={data.By} installs={data.Installs} />
          </p>
          <p className="muted store-desc">{data.Desc}</p>
        </div>
        {data.Mine && (
          <Link className="btn ghost" to={`/apps/${data.ID}`}>
            Edit app
          </Link>
        )}
      </div>
      {data.Mine && !data.Public && <Notice>Only you can see this page until you publish the app.</Notice>}

      <div className="grid two wide-left">
        <div className="stack">
          <section className="grid cards">
            {(data.Screens ?? []).map((s, i) => (
              <div className="card" key={s.id ?? i}>
                <div className="device-thumb">
                  <DeviceScreen s={s} />
                </div>
              </div>
            ))}
          </section>
          {data.Setup && (
            <section className="card">
              <h3>README</h3>
              <Markdown text={data.Setup} />
            </section>
          )}
        </div>

        <aside className="stack">
          <section className="card">
            <h3>Install</h3>
            {devices.length ? (
              devices.map((d) => (
                <div className="install-row" key={d.ID}>
                  <span className="grow">
                    <strong>{d.Name}</strong>
                    {d.NeedsFW && <div className="warn small">Shows up once it updates itself to firmware {d.NeedsFW}.</div>}
                  </span>
                  {d.Installed ? (
                    <PostButton action={`/api/apps/${data.ID}/uninstall`} data={{ device: d.ID, back }} className="btn ghost small">
                      Uninstall
                    </PostButton>
                  ) : (
                    <PostButton action={`/api/apps/${data.ID}/install`} data={{ device: d.ID, back }}>
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

          {(needs.length > 0 || personal.length > 0) && (
            <section className="card stack">
              <h3>Setup</h3>
              {needs.length > 0 && (
                <p className="muted small">
                  This app shows your own data ({needs.map((n) => `“${n}”`).join(', ')}). Install it to get your personal push link.
                </p>
              )}
              {personal.map((p) => (
                <div key={p.PushURL} className="stack">
                  <label>
                    Your push link{personal.length > 1 && ` for “${p.Name}”`}
                    <span className="command">
                      <input readOnly value={p.PushURL} className="mono grow" onClick={(e) => e.currentTarget.select()} />
                      <button type="button" className="btn ghost small" onClick={() => copy(p.PushURL)}>
                        Copy
                      </button>
                    </span>
                  </label>
                  <p className="muted small">
                    Only your boards show what you send here. Last data: {ago(p.LastFetchedAt)}.
                    {!p.LastFetchedAt && ' Until then the previews show example data.'}
                  </p>
                  {p.Data && (
                    <details>
                      <summary className="small">Latest data</summary>
                      <pre className="json">{p.Data}</pre>
                    </details>
                  )}
                </div>
              ))}
            </section>
          )}
        </aside>
      </div>
    </>
  )
}
