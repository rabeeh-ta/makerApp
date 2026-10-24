import { Loading } from '../components/common'
import { ago, plural, useTitle } from '../lib/format'
import { Link, useData } from '../lib/router'
import type { Source } from '../lib/types'

export function Sources() {
  useTitle('Data sources')
  const { data, error } = useData<{ Sources: (Source & { ScreenCount: number })[] | null }>('/api/sources')
  if (!data) return <Loading error={error} />
  const list = data.Sources ?? []
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Data sources</h1>
        </div>
        <Link className="btn" to="/sources/new">New source</Link>
      </div>
      {list.length ? (
        <div className="table card">
          <div className="tr th">
            <span>Name</span>
            <span>Type</span>
            <span>Refresh</span>
            <span>Last update</span>
            <span>Used by</span>
          </div>
          {list.map((s) => (
            <Link className="tr" to={`/sources/${s.ID}`} key={s.ID}>
              <span>
                <strong>{s.Name}</strong>
                {s.Kind === 'http' && (
                  <>
                    <br />
                    <span className="muted small mono trunc">
                      {s.Method} {s.URL}
                    </span>
                  </>
                )}
              </span>
              <span>{s.Kind === 'http' ? 'Request' : s.Kind === 'static' ? 'Fixed data' : 'Webhook'}</span>
              <span>{s.Kind === 'http' ? `every ${s.IntervalSec}s` : '—'}</span>
              <span>{s.LastError ? <span className="badge off">error</span> : ago(s.LastFetchedAt)}</span>
              <span>{plural(s.ScreenCount, 'screen')}</span>
            </Link>
          ))}
        </div>
      ) : (
        <div className="card empty">
          <h3>No data sources yet</h3>
          <Link className="btn" to="/sources/new">Add a data source</Link>
        </div>
      )}
    </>
  )
}
