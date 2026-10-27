import apiClient from './client'

export interface IQConnection {
  source: 'site' | 'custom'
  key_id?: number
  base_url?: string
  api_key?: string
}
export interface IQTestInput extends IQConnection {
  kind: 'candy' | 'pelican'
  model: string
  effort: string
  service_tier: string
  stream: boolean
}
export type IQTestConfig = Omit<IQTestInput, 'api_key'>
export interface IQUsage {
  input_tokens: number
  output_tokens: number
  cached_tokens: number | null
  reasoning_tokens: number | null
}
export interface IQTest {
  id: string
  config: IQTestConfig
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
  verdict: '' | 'passed' | 'incorrect' | 'ungraded' | 'review'
  basis: string
  error_code: string
  result: { output: string; usage: IQUsage | null; duration_ms: number; service_tier?: string }
  reference_cost_usd: number | null
  created_at: string
  deadline_at: string
  finished_at: string | null
}
export interface IQModel {
  id: string
  efforts: string[]
}
export interface IQKey {
  id: number
  name: string
  masked: string
}
export interface IQOptions {
  prompts: { candy: string; pelican: string }
}
export interface IQHistory {
  items: IQTest[]
  has_more: boolean
}

export async function startIQTest(
  input: IQTestInput & { request_id: string },
  signal?: AbortSignal
): Promise<IQTest> {
  return (await apiClient.post<IQTest>('/iq-tests', input, { signal })).data
}
export async function listIQTests(offset = 0, signal?: AbortSignal): Promise<IQHistory> {
  return (await apiClient.get<IQHistory>('/iq-tests', { params: { offset }, signal })).data
}
export async function getIQTest(id: string, signal?: AbortSignal): Promise<IQTest> {
  return (await apiClient.get<IQTest>(`/iq-tests/${encodeURIComponent(id)}`, { signal })).data
}
export async function cancelIQTest(id: string, signal?: AbortSignal): Promise<IQTest> {
  return (await apiClient.delete<IQTest>(`/iq-tests/${encodeURIComponent(id)}`, { signal })).data
}
export async function discoverIQModels(input: IQConnection, signal?: AbortSignal): Promise<IQModel[]> {
  return (await apiClient.post<IQModel[]>('/iq-tests/models', input, { signal, timeout: 25000 })).data
}
export async function getIQTestKeys(signal?: AbortSignal): Promise<IQKey[]> {
  return (await apiClient.get<IQKey[]>('/iq-tests/keys', { signal })).data
}
export async function getIQTestOptions(signal?: AbortSignal): Promise<IQOptions> {
  return (await apiClient.get<IQOptions>('/iq-tests/options', { signal })).data
}
