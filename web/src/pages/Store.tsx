import { useState } from 'react'
import { Byline, Loading } from '../components/common'
import { DeviceScreen, Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, useData, useInterval } from '../lib/router'
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

// Store is the App Store: a banner of featured apps, search and category
// filters, then Games, Tools and the community's apps. Each app opens its own
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
      <div className="page-head">
        <div>
          <h1>App Store</h1>
          <p className="muted small">Games, tools and apps people made for the makerDevice. Install any of them on your board in a click.</p>
        </div>
      </div>
      {featured.length > 0 && <Banner apps={featured} />}
      <div className="store-filters">
        <label className="store-search">
          <span className="sr-only">Search apps</span>
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-4-4" />
          </svg>
          <input type="search" placeholder="Search apps" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <fieldset className="seg chips">
          <legend className="sr-only">Category</legend>
          {['All', ...categories].map((c) => (
            <label key={c}>
              <input type="radio" name="category" checked={category === c} onChange={() => setCategory(c)} />
              {c} <span className="count">{inCategory(c).length}</span>
            </label>
          ))}
        </fieldset>
      </div>

      {!q && category === 'All' ? (
        <>
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

// Banner slides through the featured apps, one every few seconds; hovering or
// focusing it holds the slide, and so does asking for reduced motion.
function Banner({ apps }: { apps: Listing[] }) {
  const [shown, setShown] = useState(0)
  const [held, setHeld] = useState(false)
  const still = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  const go = (n: number) => setShown((n + apps.length) % apps.length)
  useInterval(() => go(shown + 1), held || still || apps.length < 2 ? null : 5000)
  return (
    <section
      className="store-banner"
      aria-roledescription="carousel"
      aria-label="Featured apps"
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}
    >
      <div className="store-banner-track" style={{ transform: `translateX(-${shown * 100}%)` }}>
        {apps.map((a, n) => (
          <Link key={a.ID} className="store-slide" to={`/store/${a.ID}`} aria-hidden={n !== shown} tabIndex={n === shown ? 0 : -1}>
            <div className="store-slide-text">
              <span className="eyebrow">Featured · {a.Category === 'Community' ? 'From the community' : a.Category.replace(/s$/, '')}</span>
              <h2>{a.Name}</h2>
              <p>{a.Desc}</p>
              <Byline by={a.By} installs={a.Installs} />
              <span className="btn store-get">{a.On?.length ? 'Installed' : 'Get'}</span>
            </div>
            <div className="store-slide-screen">{a.Preview && <DeviceScreen s={a.Preview} />}</div>
          </Link>
        ))}
      </div>
      {apps.length > 1 && (
        <>
          <button type="button" className="store-banner-nav prev" aria-label="Previous app" onClick={() => go(shown - 1)}>
            ‹
          </button>
          <button type="button" className="store-banner-nav next" aria-label="Next app" onClick={() => go(shown + 1)}>
            ›
          </button>
          <div className="store-dots">
            {apps.map((a, n) => (
              <button key={a.ID} type="button" aria-label={`Show ${a.Name}`} aria-current={n === shown} className={n === shown ? 'on' : undefined} onClick={() => go(n)} />
            ))}
          </div>
        </>
      )}
    </section>
  )
}

function AppCard({ a }: { a: Listing }) {
  return (
    <Link className="card store-card" to={`/store/${a.ID}`}>
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
