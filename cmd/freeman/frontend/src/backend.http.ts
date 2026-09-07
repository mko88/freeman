// The HTTP-transport implementation of $backend (see vite.config.ts's
// alias) — talks to internal/httpapi's REST routes instead of Wails
// bindings, for the web build served by cmd/freeman-server. Same function
// signatures as backend.wails.ts, using the same (Wails-independent)
// generated types from wailsjs/go/models.
import type { core, domain, httpengine } from '../wailsjs/go/models'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const payload = await res.json().catch(() => null)
    throw new Error(payload?.error || `${method} ${path}: ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export function CurrentWorkspace(): Promise<core.WorkspaceInfo> {
  return request('GET', '/api/workspace')
}

export function OpenWorkspace(_root: string): Promise<core.WorkspaceInfo> {
  return Promise.reject(new Error('OpenWorkspace is not available in web mode — the server has a fixed workspace'))
}

export function SelectWorkspaceFolder(): Promise<string> {
  return Promise.reject(new Error('SelectWorkspaceFolder is not available in web mode'))
}

export function GetCollection(id: string): Promise<domain.Collection> {
  return request('GET', `/api/collections/${encodeURIComponent(id)}`)
}

export function CreateCollection(name: string): Promise<domain.Collection> {
  return request('POST', '/api/collections', { name })
}

export function RenameCollection(id: string, name: string): Promise<domain.Collection> {
  return request('PATCH', `/api/collections/${encodeURIComponent(id)}`, { name })
}

export function DeleteCollection(id: string): Promise<void> {
  return request('DELETE', `/api/collections/${encodeURIComponent(id)}`)
}

export function SaveRequest(collectionId: string, item: domain.Item): Promise<domain.Item> {
  return request('POST', `/api/collections/${encodeURIComponent(collectionId)}/requests`, item)
}

export function DeleteRequest(collectionId: string, itemId: string): Promise<void> {
  return request(
    'DELETE',
    `/api/collections/${encodeURIComponent(collectionId)}/requests/${encodeURIComponent(itemId)}`,
  )
}

export function GetEnvironment(id: string): Promise<domain.Environment> {
  return request('GET', `/api/environments/${encodeURIComponent(id)}`)
}

export function SaveEnvironment(env: domain.Environment): Promise<domain.Environment> {
  return request('POST', '/api/environments', env)
}

export function DeleteEnvironment(id: string): Promise<void> {
  return request('DELETE', `/api/environments/${encodeURIComponent(id)}`)
}

export function ExecuteRequest(
  collectionId: string,
  itemId: string,
  environmentId: string,
): Promise<httpengine.Response> {
  return request('POST', '/api/execute', { collectionId, itemId, environmentId })
}

export async function GenerateRequestCode(
  item: domain.Item,
  environmentId: string,
  format: string,
): Promise<string> {
  const res = await request<{ code: string }>('POST', '/api/codegen', { item, environmentId, format })
  return res.code
}

export function GetTheme(): Promise<Record<string, string>> {
  return request('GET', '/api/theme')
}

export function GetVersion(): Promise<Record<string, string>> {
  return request('GET', '/api/version')
}

// The response cache (see internal/wailsapp/responsecache.go) is
// desktop-only — it lives under the desktop app's workspace, which a
// server that just returns bodies over HTTP has no equivalent for.
export function GetCachedResponse(_itemId: string): Promise<httpengine.Response> {
  return Promise.reject(new Error('GetCachedResponse is not available in web mode — the server does not cache responses'))
}

export function ClearResponseCache(): Promise<void> {
  return Promise.reject(new Error('ClearResponseCache is not available in web mode — the server does not cache responses'))
}

export function ClearCachedResponse(_itemId: string): Promise<void> {
  return Promise.reject(new Error('ClearCachedResponse is not available in web mode — the server does not cache responses'))
}

export function OpenResponseCacheExternally(_itemId: string): Promise<void> {
  return Promise.reject(new Error('OpenResponseCacheExternally is not available in web mode — the server does not cache responses'))
}

export function GetResponseCachePath(_itemId: string): Promise<string> {
  return Promise.reject(new Error('GetResponseCachePath is not available in web mode — the server does not cache responses'))
}

export function GetResponseCacheDataURI(_itemId: string): Promise<string> {
  return Promise.reject(new Error('GetResponseCacheDataURI is not available in web mode — the server does not cache responses'))
}

export function OpenResponseCacheInFileExplorer(_itemId: string): Promise<void> {
  return Promise.reject(new Error('OpenResponseCacheInFileExplorer is not available in web mode — the server does not cache responses'))
}
