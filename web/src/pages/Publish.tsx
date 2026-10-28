import { useState } from 'react'
import { Byline, ErrorNotice, Loading } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm } from '../lib/router'
import type { App, Screen } from '../lib/types'

type Data = {
  App: App
  Screens: { S: Screen }[] | null
  Username: string
  Installs: number
  Sources: { ID: string; Name: string; Personal: boolean; HasExample: boolean }[] | null
}

// Publish puts an app in the App Store, or edits its listing once it's there:
// the listing, the data people will see, a preview, then publish.
export function Publish({ id }: { id: string }) {
  const { data, error, stamp } = useData<Data>(`/api/apps/${id}`)
  useTitle(data && `Publish ${data.App.Name}`)
  if (!data) return <Loading error={error} />
  return <Flow key={stamp} data={data} />
}

function Flow({ data }: { data: Data }) {
  const app = data.App
  const published = app.PublishedAt > 0
  const screens = data.Screens ?? []
  const sources = data.Sources ?? []
  const { error, busy, submit } = useForm()
  const [name, setName] = useState(data.Username)
  const [desc, setDesc] = useState(app.Description)
  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/apps">My apps</Link> / <Link to={`/apps/${app.ID}`}>{app.Name}</Link> /
          </p>
          <h1>{published ? 'Store listing' : 'Publish to the App Store'}</h1>
          <p className="muted small">
            {published
              ? 'It’s in the App Store. Changes to the listing show right away, and changes to the app reach everyone’s boards.'
              : 'Anyone can find it in the App Store and install it, straight away. You can unpublish it any time.'}
          </p>
        </div>
        {published && <span className="pill ok">Public</span>}
      </div>
      <ErrorNotice error={error} />

      <form onSubmit={submit(`/api/apps/${app.ID}/publish`)} className="stack publish-flow">
        <section className="card flow-step">
          <span className="step">1</span>
          <div className="stack">
            <h3>Store listing</h3>
            {!data.Username && (
              <label>
                Your public name <span className="muted small">your apps show as @name; you pick it once</span>
                <input name="username" required pattern="[a-zA-Z0-9_\-]{3,20}" maxLength={20} placeholder="e.g. rabeeh" autoComplete="username"
                  value={name} onChange={(e) => setName(e.target.value)} />
              </label>
            )}
            <label>
              Short description <span className="muted small">{desc.length}/140</span>
              <input name="description" required maxLength={140} value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="What it shows" />
            </label>
            <label>
              Setup instructions <span className="muted small">optional: what people do after installing it, like a script to run</span>
              <textarea name="setup" rows={8} maxLength={4000} defaultValue={app.Setup} className="mono" />
            </label>
            {sources.some((s) => s.Personal) && (
              <p className="muted small">Write {'{{push_url}}'} where each person’s own push link goes; the App Store page fills it in for them.</p>
            )}
          </div>
        </section>

        <section className="card flow-step">
          <span className="step">2</span>
          <div className="stack">
            <h3>Data and privacy</h3>
            {sources.length === 0 && <p className="muted small">It shows no data from your sources.</p>}
            {sources.map((s) => (
              <div className="install-row" key={s.ID}>
                <div className="grow">
                  <strong>{s.Name}</strong>
                  {s.Personal ? (
                    <div className="muted small">
                      Personal: everyone sends their own data and gets their own push link. Your data stays yours.
                      {!s.HasExample && <span className="warn"> Add example data so the store preview isn’t blank.</span>}
                    </div>
                  ) : (
                    <div className="warn small">Shared: everyone who installs it sees this data, fetched with your account.</div>
                  )}
                </div>
                <Link className="btn ghost small" to={`/sources/${s.ID}`}>
                  Edit
                </Link>
              </div>
            ))}
          </div>
        </section>

        <section className="card flow-step">
          <span className="step">3</span>
          <div className="stack">
            <h3>Preview</h3>
            <div className="publish-preview">
              <div className="card screen-card">
                <Thumb s={screens[0]?.S} empty="Add a screen first" />
                <div className="row between">
                  <strong>{app.Name}</strong>
                  <Byline by={name || 'you'} installs={data.Installs} />
                </div>
                <div className="muted small">{desc || 'Your description'}</div>
              </div>
              <p className="muted small">
                How it looks in the App Store. Its page shows all {screens.length} screens{sources.some((s) => s.Personal) && ', with example data until people send their own'}.
              </p>
            </div>
          </div>
        </section>

        <section className="card flow-step">
          <span className="step">4</span>
          <div className="row between wrap">
            <h3>{published ? 'Save changes' : 'Publish'}</h3>
            <div className="row">
              {published && (
                <Link className="btn ghost" to={`/store/${app.ID}`}>
                  View in App Store
                </Link>
              )}
              <button className="btn" disabled={busy || screens.length === 0}>
                {published ? 'Save' : `Publish${name ? ` as @${name.toLowerCase()}` : ''}`}
              </button>
            </div>
          </div>
        </section>
      </form>

      {published && (
        <p className="after-grid">
          <PostButton action={`/api/apps/${app.ID}/unpublish`} className="btn danger small" confirm={`Unpublish “${app.Name}”? It comes off the App Store and off other people's boards.`}>
            Unpublish
          </PostButton>
        </p>
      )}
    </>
  )
}
