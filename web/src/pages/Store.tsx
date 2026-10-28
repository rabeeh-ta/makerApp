import { useState } from 'react'
import { Byline, Loading } from '../components/common'
import { DeviceScreen, Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, useData } from '../lib/router'
import type { Device, Screen } from '../lib/types'

type Listing = {
  ID: string
  Name: string
  Desc: string
  By: string
  Category: string
  Installs: number
  Preview: Screen | null
  On: Device[] | null
}

const categories = ['Games', 'Tools', 'Community'] as const
const sections: Record<(typeof categories)[number], { title: string; note: string }> = {
  Games: { title: 'Games', note: 'Arcade classics for three buttons. Hold the middle one to leave.' },
  Tools: { title: 'Tools', note: 'Everyday helpers from makerInc.' },
  Community: { title: 'From the community', note: 'Apps people built and published. Some show your own data.' },
}

// Store is the App Store: featured apps, then Games, Tools and the
// community's apps, with search and category filters. Each app opens its own
// page to install and set it up.
export function Store() {
  useTitle('App Store')
  const { data, error } = useData<{ Apps: Listing[] | null; Featured: string[] | null }>('/api/store')
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState('All')
  if (!data) return <Loading error={error} />
  const apps = data.Apps ?? []
  const q = query.trim().toLowerCase()
  const inCategory = (c: string) => apps.filter((a) => c === 'All' || a.Category === c)
  const matches = inCategory(category).filter((a) => !q || [a.Name, a.Desc, a.By].some((t) => t.toLowerCase().includes(q)))
  const featured = (data.Featured ?? []).map((id) => apps.find((a) => a.ID === id)).filter((a): a is Listing => !!a)
  return (
    <>
      <div className="page-head store-head">
        <div>
          <h1>App Store</h1>
          <p className="muted small">Games, tools and apps people made for the makerDevice. Install any of them on your board in a click.</p>
        </div>
        <label className="store-search">
          <span className="sr-only">Search apps</span>
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-4-4" />
          </svg>
          <input type="search" placeholder="Search apps" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
      </div>
      <fieldset className="seg chips store-chips">
        <legend className="sr-only">Category</legend>
        {['All', ...categories].map((c) => (
          <label key={c}>
            <input type="radio" name="category" checked={category === c} onChange={() => setCategory(c)} />
            {c} <span className="count">{inCategory(c).length}</span>
          </label>
        ))}
      </fieldset>

      {!q && category === 'All' ? (
        <>
          {featured.length > 0 && <Featured apps={featured} />}
          {categories.map((c) => {
            const list = inCategory(c)
            return (
              <section className="store-section" key={c}>
                <div className="row between store-section-head">
                  <div>
                    <h2>{sections[c].title}</h2>
                    <p className="muted small">{sections[c].note}</p>
                  </div>
                  {list.length > 4 && (
                    <button type="button" className="link" onClick={() => setCategory(c)}>
                      See all {list.length}
                    </button>
                  )}
                </div>
                {list.length ? (
                  <div className="grid store-grid">
                    {list.slice(0, 4).map((a) => (
                      <AppCard key={a.ID} a={a} />
                    ))}
                  </div>
                ) : (
                  <div className="card empty">
                    <p className="muted">
                      Nobody has published an app yet. Build one in <Link to="/apps">My apps</Link> and publish it.
                    </p>
                  </div>
                )}
              </section>
            )
          })}
        </>
      ) : (
        <section className="store-section">
          <p className="muted small">
            {plural(matches.length, 'app')}
            {q && ` for “${query.trim()}”`}
            {category !== 'All' && ` in ${category}`}
          </p>
          {matches.length ? (
            <div className="grid store-grid">
              {matches.map((a) => (
                <AppCard key={a.ID} a={a} />
              ))}
            </div>
          ) : (
            <div className="card empty">
              <p className="muted">No apps match. Try another word or category.</p>
            </div>
          )}
        </section>
      )}
    </>
  )
}

// Featured is the first featured app, large, with the next two beside it.
function Featured({ apps }: { apps: Listing[] }) {
  const [hero, ...rest] = apps
  return (
    <section className="store-featured" aria-label="Featured">
      <Link className="card store-hero" to={`/store/${hero.ID}`}>
        <div className="store-hero-screen">{hero.Preview && <DeviceScreen s={hero.Preview} />}</div>
        <div className="stack tight">
          <span className="eyebrow">Featured</span>
          <h2>{hero.Name}</h2>
          <Byline by={hero.By} installs={hero.Installs} />
          <p className="muted">{hero.Desc}</p>
          <span className="btn store-get">{hero.On?.length ? 'Installed' : 'Get'}</span>
        </div>
      </Link>
      {rest.length > 0 && (
        <div className="store-featured-side">
          {rest.slice(0, 2).map((a) => (
            <AppCard key={a.ID} a={a} compact />
          ))}
        </div>
      )}
    </section>
  )
}

function AppCard({ a, compact }: { a: Listing; compact?: boolean }) {
  return (
    <Link className={compact ? 'card store-card compact' : 'card store-card'} to={`/store/${a.ID}`}>
      <Thumb s={a.Preview} empty="No preview" />
      <div className="stack tight">
        <div className="row between">
          <strong>{a.Name}</strong>
          {a.On?.length ? <span className="pill ok">Installed</span> : null}
        </div>
        <Byline by={a.By} installs={a.Installs} />
        <p className="muted small clamp">{a.Desc}</p>
      </div>
    </Link>
  )
}
