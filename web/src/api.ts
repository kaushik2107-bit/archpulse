import type { CatalogEntry, ImportedArchitecture, SimulationJob, SimulationPayload } from './types'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(body.error ?? `Request failed with status ${response.status}`)
  }
  return body as T
}

export async function getCatalog(): Promise<CatalogEntry[]> {
  const response = await request<{ services: CatalogEntry[] }>('/api/catalog')
  return response.services
}

export async function validateArchitecture(payload: SimulationPayload): Promise<void> {
  await request('/api/validate', { method: 'POST', body: JSON.stringify(payload) })
}

export async function importYAML(contents: string): Promise<ImportedArchitecture> {
  const response = await fetch('/api/architectures/import-yaml', {
    method: 'POST',
    headers: { 'Content-Type': 'application/yaml' },
    body: contents,
  })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.error ?? `Import failed with status ${response.status}`)
  return body as ImportedArchitecture
}

export async function startSimulation(payload: SimulationPayload, seed: number): Promise<SimulationJob> {
  return request<SimulationJob>('/api/simulations', {
    method: 'POST',
    body: JSON.stringify({ ...payload, seed }),
  })
}

export async function getSimulation(simulationID: string): Promise<SimulationJob> {
  return request<SimulationJob>(`/api/simulations/${encodeURIComponent(simulationID)}`)
}

export async function cancelSimulation(simulationID: string): Promise<void> {
  await request(`/api/simulations/${encodeURIComponent(simulationID)}`, { method: 'DELETE' })
}
