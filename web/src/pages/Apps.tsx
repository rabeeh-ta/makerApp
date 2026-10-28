import { Byline, ErrorNotice, Loading, MyAppsHead } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, useData, useForm } from '../lib/router'
import type { App, Device, Me, Screen } from '../lib/types'

type Data = {
  Apps: (App & { ScreenCount: number; Installs: number; Preview: Screen | null; On: Device[] | null })[] | null
  Username: string
}

export function Apps({ me }: { me: Me }) {
  useTitle('My apps')
  const { data, error } = useData<Data>('/api/apps')
  const newApp = useForm()
  if (!data) return <Loading error={error} />
  const apps = data.Apps ?? []
  return (
    <>
      <MyAppsHead
        tab="apps"
        intro="Apps you made from screens. Install them on a board, or publish them in the App Store for anyone to install."
        actions={
          <>
            <Link className="btn ghost" to="/connect">Build with AI</Link>
            <form onSubmit={newApp.submit('/api/apps')} className="row">
              <input name="name" placeholder="New app name" maxLength={40} required className="inline-input" />
              <button className="btn" disabled={newApp.busy}>New app</button>
            </form>
          </>
        }
      />
      <ErrorNotice error={newApp.error} />

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
              {a.MadeBy && <span className="pill made-by">Made with {a.MadeBy}</span>}
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

      <p className="muted small after-grid">
        Looking for clocks, games and apps other people made? <Link to="/store">Browse the App Store</Link>.
      </p>
    </>
  )
}
