// toast shows a short message at the bottom of the page.
let timer: ReturnType<typeof setTimeout> | undefined

export function toast(msg: string) {
  let t = document.getElementById('page-toast')
  if (!t) {
    t = document.createElement('div')
    t.id = 'page-toast'
    document.body.appendChild(t)
  }
  t.textContent = msg
  t.classList.add('show')
  clearTimeout(timer)
  timer = setTimeout(() => t.classList.remove('show'), 1600)
}
