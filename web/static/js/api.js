// API module
export const API_BASE = '/api/v1';

async function apiCall(endpoint, options = {}) {
    try {
        const response = await fetch(`${API_BASE}${endpoint}`, options);
        return await response.json();
    } catch (error) {
        console.error('API call failed:', error);
        return { success: false, error: { message: error.message } };
    }
}

export const tasksAPI = {
    list: () => apiCall('/tasks'),
    get: (id) => apiCall(`/tasks/${id}`),
    create: (task) => apiCall('/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(task)
    }),
    update: (id, task) => apiCall(`/tasks/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(task)
    }),
    delete: (id) => apiCall(`/tasks/${id}`, { method: 'DELETE' }),
    execute: (id, dryRun = false) => apiCall(`/tasks/${id}/execute${dryRun ? '?dry_run=true' : ''}`, {
        method: 'POST'
    })
};

export const backendsAPI = {
    list: () => apiCall('/backends'),
    get: (id) => apiCall(`/backends/${id}`),
    create: (backend) => apiCall('/backends', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(backend)
    }),
    update: (id, backend) => apiCall(`/backends/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(backend)
    }),
    delete: (id) => apiCall(`/backends/${id}`, { method: 'DELETE' }),
    test: (id) => apiCall(`/backends/${id}/test`, { method: 'POST' })
};

export const executionsAPI = {
    list: (perPage = 20) => apiCall(`/executions?per_page=${perPage}`),
    get: (id) => apiCall(`/executions/${id}`),
    clearHistory: () => apiCall('/executions', { method: 'DELETE' })
};

export const systemAPI = {
    stats: () => apiCall('/system/stats'),
    settings: () => apiCall('/system/settings'),
    updateSettings: (settings) => apiCall('/system/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings)
    })
};

export const sourcesAPI = {
    list: (path = '') => apiCall(`/sources${path ? `?path=${encodeURIComponent(path)}` : ''}`)
};
