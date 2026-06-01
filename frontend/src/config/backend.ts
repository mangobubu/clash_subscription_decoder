const legacyBackendBaseUrl = 'http://localhost:8080'

const backendBaseUrl = (import.meta.env.VITE_BACKEND_BASE_URL ?? '').trim().replace(/\/+$/, '')

const withLeadingSlash = (path: string) => (path.startsWith('/') ? path : `/${path}`)

export const buildBackendUrl = (path: string) => {
  const normalizedPath = withLeadingSlash(path)
  return backendBaseUrl ? `${backendBaseUrl}${normalizedPath}` : normalizedPath
}

export const buildPublicBackendUrl = (path: string) => {
  const normalizedUrl = buildBackendUrl(path)
  if (/^https?:\/\//i.test(normalizedUrl)) {
    return normalizedUrl
  }
  return `${window.location.origin}${normalizedUrl}`
}

export const normalizeBackendUrl = (url?: string) => {
  if (!url) {
    return url
  }

  if (url.startsWith(legacyBackendBaseUrl)) {
    return buildBackendUrl(url.slice(legacyBackendBaseUrl.length))
  }

  if (backendBaseUrl && url.startsWith('/')) {
    return `${backendBaseUrl}${url}`
  }

  return url
}
