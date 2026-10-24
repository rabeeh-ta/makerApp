import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development, run `go run .` (API on :3000) and `npm run dev` here, then
// open http://localhost:5173. Vite forwards the API and the routes boards and
// the flasher use to the Go server.
const api = 'http://127.0.0.1:3000'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': api,
      '^/flash/.+': api,
      '/fw': api,
      '/games': api,
      '/push': api,
      '/ws': { target: api, ws: true },
    },
  },
})
