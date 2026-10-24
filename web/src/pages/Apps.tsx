import { Byline, ErrorNotice, Loading } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm } from '../lib/router'
import type { App, Device, Me, Screen } from '../lib/types'

type Data = {
  Apps: (App & { ScreenCount: number; Installs: number; Preview: Screen | null; On: Device[] | null })[] | null
  Public: { ID: string; Name: string; Desc: string; By: string; Installs: number; Preview: Screen | null; On: Device[] | null }[] | null
  Devices: Device[] | null
  Username: string
}

export function Apps({ me }: { me: Me }) {
  useTitle('Apps')
  const { data, error } = useData<Data>('/api/apps')
  const newApp = useForm()
  if (!data) return <Loading error={error} />
  const apps = data.Apps ?? []
  const devices = data.Devices ?? []
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Apps</h1>
          <p className="muted small">
            Install apps on a board and they show up in its Apps drawer. Make your own from screens, and publish them for anyone to install.
          </p>
        </div>
        <form onSubmit={newApp.submit('/api/apps')} className="row">
          <input name="name" placeholder="New app name" maxLength={40} required className="inline-input" />
          <button className="btn" disabled={newApp.busy}>New app</button>
        </form>
      </div>
      <ErrorNotice error={newApp.error} />

      <h3 className="section-title">Your apps</h3>
      {apps.length ? (
        <div className="grid cards">
          {apps.map((a) => (
            <Link className="card screen-card screen-link" to={`/apps/${a.ID}`} key={a.ID}>
              <Thumb s={a.Preview} empty="No screens yet" />
              <div className="row between">
                <strong>{a.Name}</strong>
                <span className="muted small">{plural(a.ScreenCount, 'screen')}</span>
              </div>
              <div className="muted small">{a.On?.length ? `On ${a.On.map((d) => d.Name).join(', ')}` : 'Not installed'}</div>
              {a.PublishedAt > 0 && (
                <div className="row public-row">
                  <span className="pill ok">Public</span>
                  <Byline by={data.Username || me.User?.Username || ''} installs={a.Installs} />
                </div>
              )}
            </Link>
          ))}
        </div>
      ) : (
        <div className="card empty">
          <h3>No apps yet</h3>
          <p className="muted">Name one above, then add screens to it.</p>
        </div>
      )}

      <h3 className="section-title">All apps</h3>
      <p className="muted small section-note">
        Made by makerApp and by people who published theirs, most installed first. In apps that use the middle button, hold it for a second to leave.
      </p>
      <div className="grid cards">
        {(data.Public ?? []).map((l) => (
          <div className="card screen-card" key={l.ID}>
            <Thumb s={l.Preview} empty="No preview" />
            <div className="row between">
              <strong>{l.Name}</strong>
              <Byline by={l.By} installs={l.Installs} />
            </div>
            <div className="muted small">{l.Desc}</div>
            <div className="card-foot">
              {devices.map((d) => (
                <div className="install-row" key={d.ID}>
                  <span className="grow small">{d.Name}</span>
                  {l.On?.some((x) => x.ID === d.ID) ? (
                    <PostButton action={`/api/apps/${l.ID}/uninstall`} data={{ device: d.ID, back: '/apps' }} className="btn ghost small">
                      Uninstall
                    </PostButton>
                  ) : (
                    <PostButton action={`/api/apps/${l.ID}/install`} data={{ device: d.ID, back: '/apps' }}>
                      Install
                    </PostButton>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
