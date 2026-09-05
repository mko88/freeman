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

export function ExecuteRequest(
  collectionId: string,
  itemId: string,
  environmentId: string,
): Promise<httpengine.Response> {
  return request('POST', '/api/execute', { collectionId, itemId, environmentId })
}

export function GetTheme(): Promise<Record<string, string>> {
  return request('GET', '/api/theme')
}

export async function GetResponseBody(path: string): Promise<string> {
  const res = await fetch(`/api/execute/body?path=${encodeURIComponent(path)}`)
  if (!res.ok) {
    const payload = await res.json().catch(() => null)
    throw new Error(payload?.error || `GET /api/execute/body: ${res.status}`)
  }
  return res.text()
}

export function OpenResponseExternally(_path: string): Promise<void> {
  return Promise.reject(new Error('OpenResponseExternally is not available in web mode — there is no local application to hand it to'))
}

export function OpenResponseInFileExplorer(_path: string): Promise<void> {
  return Promise.reject(
    new Error('OpenResponseInFileExplorer is not available in web mode — there is no local file manager to hand it to'),
  )
}
