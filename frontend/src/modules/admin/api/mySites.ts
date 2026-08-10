import type {
  RunAutoPricingRequest,
  RunAutoPricingResponse,
  MySiteMapping,
  MySiteMappingOptionsResponse,
  MySiteStatus,
  RealBindRequest,
  RealConnectRequest,
  RealConnectResponse,
  RealConnection,
  RealDisconnectRequest,
  UpstreamKeyItem,
  AdminResourceOption,
} from '../types/mySites'
import { isEndpointUnsupported, requestJson as sharedRequestJson } from '@/lib/apiClient'


const normalizeMappings = (value: unknown): MySiteMapping[] => {
  if (!Array.isArray(value)) return []
  return value.flatMap((entry) => {
    if (entry == null || typeof entry !== 'object') return []
    const mapping = entry as MySiteMapping
    if (typeof mapping.ownGroup !== 'string' || !mapping.ownGroup.trim()) return []
    const upstreamTargets = Array.isArray(mapping.upstreamTargets)
      ? mapping.upstreamTargets.filter(target => (
          target != null &&
          typeof target.siteId === 'string' &&
          typeof target.groupName === 'string'
        ))
      : []
    return [{ ...mapping, upstreamTargets }]
  })
}

const normalizeStatus = (status: MySiteStatus): MySiteStatus => ({
  ...status,
  ...(Object.prototype.hasOwnProperty.call(status, 'mappings')
    ? { mappings: normalizeMappings(status.mappings) }
    : {}),
})

const normalizeMappingOptions = (response: MySiteMappingOptionsResponse): MySiteMappingOptionsResponse => ({
  ...response,
  ownGroups: Array.isArray(response.ownGroups) ? response.ownGroups : [],
  mappings: normalizeMappings(response.mappings),
  staleOwnGroups: Array.isArray(response.staleOwnGroups) ? response.staleOwnGroups : [],
  staleTargets: Array.isArray(response.staleTargets) ? response.staleTargets : [],
})

const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.mySites.errors.network',
    request: 'admin.mySites.errors.request',
  })

export const getMySiteMappingOptions = async (): Promise<MySiteMappingOptionsResponse> => (
  normalizeMappingOptions(await requestJson<MySiteMappingOptionsResponse>('/my-sites/mapping-options'))
)

export const saveMySiteMappings = async (mappings: MySiteMapping[]): Promise<MySiteStatus> => (
  normalizeStatus(await requestJson<MySiteStatus>('/my-sites/mappings', {
    method: 'PUT',
    body: JSON.stringify({ mappings }),
  }))
)

export const realConnect = async (req: RealConnectRequest): Promise<RealConnectResponse> => (
  requestJson<RealConnectResponse>('/my-sites/real-connect', {
    method: 'POST',
    body: JSON.stringify(req),
  })
)

export const listRealConnections = async (): Promise<RealConnection[]> =>
  requestJson<RealConnection[]>('/my-sites/real-connections')

export const listUpstreamKeys = async (siteId: string, groupId: string, groupName: string): Promise<UpstreamKeyItem[]> => {
  const params = new URLSearchParams({ siteId, groupId, groupName })
  const items = await requestJson<UpstreamKeyItem[]>(`/my-sites/upstream-keys?${params.toString()}`)
  return Array.isArray(items)
    ? items.map(item => ({
        ...item,
        // Older backends returned the full key. Keep it only as an internal
        // compatibility fallback; the UI renders the non-secret preview.
        keyPreview: item.keyPreview || (item.key ? `${item.key.slice(0, 6)}...${item.key.slice(-4)}` : ''),
      }))
    : []
}

export const listAdminResources = async (groupId: string): Promise<AdminResourceOption[]> => {
  const items = await requestJson<AdminResourceOption[]>(`/my-sites/admin-resources?groupId=${encodeURIComponent(groupId)}`)
  return Array.isArray(items) ? items : []
}

export const realBind = async (req: RealBindRequest): Promise<RealConnectResponse> => (
  requestJson<RealConnectResponse>('/my-sites/real-bind', {
    method: 'POST',
    body: JSON.stringify(req),
  })
)

export const realDisconnect = async (req: RealDisconnectRequest): Promise<void> => {
  await requestJson<{ ok: boolean }>('/my-sites/real-disconnect', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export const runAutoPricing = async (req: RunAutoPricingRequest): Promise<RunAutoPricingResponse> => {
  const response = await requestJson<RunAutoPricingResponse>('/my-sites/auto-pricing/run', {
    method: 'POST',
    body: JSON.stringify(req),
  })
  return {
    ...response,
    mapping: normalizeMappings([response.mapping])[0] ?? response.mapping,
  }
}

// 新后端支持单分组原子更新（PATCH）。只有当后端确实没有这个接口/方法时
// （404/405）才降级为旧版全量 PUT，保证滚动升级期间可用。
//
// 这里必须按 HTTP 状态码判断，不能再按错误文案判断：早期实现只要拿到通用的
// admin.mySites.errors.request 就降级，于是任何一次校验失败（400）都会把
// 当前浏览器内存里的整份 mappings 全量写回，静默覆盖其他标签页/其他人的改动，
// 而且真正的报错原因也被这次「成功的」全量保存掩盖掉了。
export const saveMySiteMapping = async (mapping: MySiteMapping, currentMappings: MySiteMapping[]): Promise<MySiteStatus> => {
  try {
    return normalizeStatus(await requestJson<MySiteStatus>('/my-sites/mappings', {
      method: 'PATCH',
      body: JSON.stringify({ mapping }),
    }))
  } catch (error) {
    if (!isEndpointUnsupported(error)) throw error
    const nextMappings = currentMappings.some(item => item.ownGroup === mapping.ownGroup)
      ? currentMappings.map(item => item.ownGroup === mapping.ownGroup ? mapping : item)
      : [...currentMappings, mapping]
    return saveMySiteMappings(nextMappings)
  }
}

export const removeMySiteMapping = async (ownGroup: string, currentMappings: MySiteMapping[]): Promise<MySiteStatus> => {
  try {
    return normalizeStatus(await requestJson<MySiteStatus>(`/my-sites/mappings/${encodeURIComponent(ownGroup)}`, { method: 'DELETE' }))
  } catch (error) {
    if (!isEndpointUnsupported(error)) throw error
    return saveMySiteMappings(currentMappings.filter(item => item.ownGroup !== ownGroup))
  }
}
