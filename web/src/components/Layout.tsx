import { post } from '../lib/api'
import { Link, navigate } from '../lib/router'
import { setTheme, themes, useTheme, type Theme } from '../lib/theme'
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
            <nav>
              <Link to="/devices" className={on('/devices', '/flash')}>Devices</Link>
              <Link to="/apps" className={on('/apps', '/screens', '/sources')}>My apps</Link>
              <Link to="/store" className={on('/store')}>App Store</Link>
              <Link to="/connect" className={on('/connect')}>MCP</Link>
            </nav>
          )}
          <div className="who">
            <ThemeButton />
            {me.User && (
              <>
                <Link to="/account" className={['avatar', on('/account')].filter(Boolean).join(' ')} title={`${me.User.Email} · Account`} aria-label="Account">
                  {me.User.Email.slice(0, 1)}
                </Link>
                <button type="button" className="link" onClick={logout}>Log out</button>
              </>
            )}
          </div>
        </div>
      </header>
      <main>{children}</main>
    </>
  )
}

const nextTheme: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' }

// ThemeButton steps through System, Light and Dark; its icon is the current one.
function ThemeButton() {
  const theme = useTheme()
  const label = (t: Theme) => themes.find((x) => x.value === t)!.label
  return (
    <button
      type="button"
      className="theme-button"
      onClick={() => setTheme(nextTheme[theme])}
      title={`Theme: ${label(theme)}. Click for ${label(nextTheme[theme])}.`}
      aria-label={`Theme: ${label(theme)}`}
    >
      <svg viewBox="0 0 24 24" aria-hidden="true">
        {theme === 'light' && (
          <>
            <circle cx="12" cy="12" r="4" />
            <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
          </>
        )}
        {theme === 'dark' && <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5Z" />}
        {theme === 'system' && (
          <>
            <rect x="3" y="4" width="18" height="13" rx="2" />
            <path d="M8 21h8M12 17v4" />
          </>
        )}
      </svg>
    </button>
  )
}
