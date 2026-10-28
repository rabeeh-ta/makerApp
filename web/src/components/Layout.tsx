import { post } from '../lib/api'
import { Link, navigate } from '../lib/router'
import type { Me } from '../lib/types'

export function Layout({ me, path, onSignOut, children }: { me: Me; path: string; onSignOut: () => void; children: React.ReactNode }) {
  const on = (...prefixes: string[]) => (prefixes.some((p) => path.startsWith(p)) ? 'on' : undefined)
  async function logout() {
    await post('/api/logout')
    onSignOut()
    navigate('/')
  }
  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Link className="brand" to="/">
            <span className="brand-mark" />
            makerApp
          </Link>
          {me.User && (
            <>
              <nav>
                <Link to="/devices" className={on('/devices', '/flash')}>Devices</Link>
                <Link to="/apps" className={on('/apps', '/screens')}>Apps</Link>
                <Link to="/sources" className={on('/sources')}>Data</Link>
                <Link to="/connect" className={on('/connect')}>AI Builder</Link>
              </nav>
              <div className="who">
                <Link to="/account" className={['avatar', on('/account')].filter(Boolean).join(' ')} title={`${me.User.Email} · Account`} aria-label="Account">
                  {me.User.Email.slice(0, 1)}
                </Link>
                <button type="button" className="link" onClick={logout}>Log out</button>
              </div>
            </>
          )}
        </div>
      </header>
      <main>{children}</main>
    </>
  )
}
