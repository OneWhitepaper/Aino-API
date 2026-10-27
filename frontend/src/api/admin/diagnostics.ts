import { apiClient } from '../client'

export interface DiagnosticMetadata {
  id: string
  user_id: number
  size_bytes: number
  created_at: string
  expires_at: string
  kind?: 'diagnostic_log' | 'usage_statistics'
}

export interface DiagnosticDetail {
  metadata: DiagnosticMetadata
  files: Record<string, string>
}

export async function listDiagnostics(page: number, pageSize: number, id: string, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: DiagnosticMetadata[]; total: number; page: number; page_size: number }>(
    '/admin/diagnostics', { params: { page, page_size: pageSize, id }, signal })
  return data
}

export async function readDiagnostic(id: string, signal?: AbortSignal) {
  const { data } = await apiClient.get<DiagnosticDetail>(`/admin/diagnostics/${encodeURIComponent(id)}`, { signal })
  return data
}

export async function deleteDiagnostic(id: string) {
  await apiClient.delete(`/admin/diagnostics/${encodeURIComponent(id)}`)
}
