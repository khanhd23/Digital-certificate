import { queryString } from '../utils/common'
import apiService from './root'

export const createDegreeTemplate = async (data: any) => {
  const res = await apiService('POST', 'templates', data)
  return res
}

export const getDegreeTemplateById = async (id: string) => {
  const res = await apiService('GET', `templates/${id}`)

  return {
    ...res.data,
    faculty_id: res.data.facultyId
  }
}

export const updateDegreeTemplate = async (id: string, data: any) => {
  const res = await apiService('PUT', `templates/${id}`, data)
  return res
}
export const searchDegreeTemplateByFaculty = async (facultyId: string) => {
  const res = await apiService('GET', `templates/faculty?faculty_id=${facultyId}`)
  return res
}

export const searchDigitalDegreeList = async (params: any) => {
  const res = await apiService('GET', queryString(['ediplomas', 'search'], params))

  return res
}

export const issueDigitalDegree = async (ediplomaId: string, templateId: string) => {
  return apiService('POST', 'ediplomas/generate', {
    ediploma_id: ediplomaId,
    template_id: templateId
  })
}

export const revokeDigitalDegree = async (ediplomaId: string, reason: string) => {
  return apiService('POST', `ediplomas/${ediplomaId}/revoke`, { reason })
}

export const createDigitalDegree = async (data: {
  student_code: string
  name: string
  certificate_type?: string
  course?: string
  education_type?: string
  gpa?: number
  graduation_rank?: string
  issue_date: string
  serial_number: string
  registration_number: string
  round_id: string
}) => {
  return apiService('POST', 'ediplomas', data)
}

export const uploadDigitalDegreesBlockchain = async (
  facultyId: string,
  roundId: string,
  certificateType: string,
  course: string
) => {
  const res = await apiService('POST', 'blockchain/push-ediploma', {
    faculty_id: facultyId,
    round_id: roundId,
    certificate_type: certificateType,
    course: course
  })
  return res
}

// Records on Fabric the revocations of one faculty of one issuance round.
export const uploadRevocationsBlockchain = async (facultyId: string, roundId: string) => {
  return apiService('POST', 'blockchain/push-revocations', { faculty_id: facultyId, round_id: roundId })
}

export type IssuanceRound = {
  id: string
  name: string
  decision_number?: string
  decision_date?: string
  note?: string
  created_at: string
}

// Newest rounds first; with a query, rounds whose name contains it; with a
// faculty, only rounds holding that faculty's diplomas.
export const searchIssuanceRounds = async (query = '', facultyId = '', limit = 5): Promise<IssuanceRound[]> => {
  const res = await apiService('GET', queryString(['issuance-rounds'], { q: query, faculty_id: facultyId, limit }))
  return res.data ?? []
}

export const createIssuanceRound = async (data: {
  name: string
  decision_number?: string
  decision_date?: string
  note?: string
}): Promise<IssuanceRound> => {
  const res = await apiService('POST', 'issuance-rounds', data)
  return res.data
}

export const assignIssuanceRound = async (data: {
  round_id: string
  faculty_id: string
  course?: string
  certificate_type?: string
}) => {
  return apiService('POST', 'ediplomas/assign-round', data)
}

export const revokeIssuanceRound = async (data: {
  faculty_id: string
  round_id: string
  reason: string
  confirm: string
}) => {
  return apiService('POST', 'ediplomas/revoke-batch', data)
}

export const getTemplateInterfaces = async () => {
  const res = await apiService('GET', 'template-samples')
  return res
}

export const getTemplateInterfaceById = async (id: string) => {
  const res = await apiService('GET', `template-samples/${id}`)
  return res
}

export const updateTemplateInterface = async (id: string, data: any) => {
  const res = await apiService('PUT', `template-samples/${id}`, data)
  return res
}

export const createTemplateInterface = async (data: any) => {
  const res = await apiService('POST', 'template-samples', data)
  return res
}

export const getDigitalDegreeFileById = async (id: string) => {
  const res = await apiService('GET', `ediplomas/file/${id}`, undefined, true, {}, true)
  return res
}

export const getDigitalDegreeById = async (id: string) => {
  const res = await apiService('GET', `ediplomas/${id}`)
  return res
}

export const getDigitalDegreesByStudent = async () => {
  const res = await apiService('GET', 'ediplomas/simple')
  return res.data
}

export const importDigitalDegreeExcel = async (data: FormData) => {
  const res = await apiService('POST', 'ediplomas/import-excel', data)
  return res
}
