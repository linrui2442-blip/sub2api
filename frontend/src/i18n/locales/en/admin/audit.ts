export default {
  audit: {
    title: 'Audit Logs',
    description: 'Records the last 7 days of management-plane operations by admins and users. Header credentials are masked and request bodies are redacted.',
    clearAll: 'Clear All',
    empty: 'No audit logs yet',
    loadFailed: 'Failed to load audit logs',
    filters: {
      all: 'All',
      q: 'Keyword',
      qPlaceholder: 'Path / action / actor email',
      actorEmail: 'Actor Email',
      action: 'Action',
      clientIp: 'Client IP',
      method: 'Method',
      authMethod: 'Auth Method',
      result: 'Result',
      resultSuccess: 'Success',
      resultFailure: 'Failure',
      startTime: 'Start Time',
      endTime: 'End Time'
    },
    columns: {
      time: 'Time',
      actor: 'Actor',
      action: 'Action',
      method: 'Method',
      result: 'Result',
      clientIp: 'Client IP',
      detail: 'Detail'
    },
    detail: {
      title: 'Audit Log Detail',
      actorRole: 'Role',
      methodPath: 'Method / Path',
      latency: 'Latency',
      requestId: 'Request ID',
      credential: 'Credential (masked)',
      userAgent: 'User-Agent',
      requestBody: 'Request Body (redacted)',
      extra: 'Extra'
    },
    clearConfirm: {
      title: 'Clear All Audit Logs',
      message: 'Clear all audit logs? This action cannot be undone.',
      success: 'Cleared {count} audit log(s)',
      failed: 'Failed to clear audit logs'
    }
  }
}
