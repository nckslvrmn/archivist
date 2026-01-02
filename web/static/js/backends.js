// Backends module
import { backendsAPI } from './api.js';
import { showToast } from './utils.js';
import { showModal, closeModal, setModalContent } from './modal.js';
import { getBackendConfigFields } from './backend-forms.js';

export function initBackends() {
    loadBackends();

    // Expose functions globally for onclick handlers
    window.showCreateBackend = showCreateBackend;
    window.testBackend = testBackend;
    window.editBackend = editBackend;
    window.deleteBackend = deleteBackend;
}

export async function loadBackends() {
    try {
        const response = await backendsAPI.list();

        if (response.success) {
            renderBackends(response.data);
        }
    } catch (error) {
        console.error('Failed to load backends:', error);
    }
}

function renderBackends(backends) {
    const container = document.getElementById('backends-list');

    if (!backends || backends.length === 0) {
        container.innerHTML = '<p class="empty-state">No backends configured. Add your first storage backend!</p>';
        return;
    }

    const template = document.getElementById('backend-card-template');
    container.innerHTML = '';

    backends.forEach(backend => {
        const card = template.content.cloneNode(true);

        // Fill in fields
        card.querySelector('[data-field="name"]').textContent = backend.name;
        card.querySelector('[data-field="type"]').textContent = backend.type.toUpperCase();

        const badge = card.querySelector('[data-field="enabled-badge"]');
        badge.textContent = backend.enabled ? 'Enabled' : 'Disabled';
        badge.className = `badge badge-${backend.enabled ? 'success' : 'disabled'}`;

        card.querySelector('[data-field="test-status"]').textContent =
            backend.last_test_status ? `Last test: ${backend.last_test_status}` : 'Never tested';

        // Wire up actions
        card.querySelector('[data-action="test"]').onclick = () => testBackend(backend.id);
        card.querySelector('[data-action="edit"]').onclick = () => editBackend(backend.id);
        card.querySelector('[data-action="delete"]').onclick = () => deleteBackend(backend.id);

        container.appendChild(card);
    });
}

async function testBackend(backendId) {
    try {
        const response = await backendsAPI.test(backendId);

        if (response.success) {
            showToast('Backend test successful!', 'success');
            loadBackends();
        } else {
            showToast('Backend test failed: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

function showCreateBackend() {
    setModalContent(`
        <h2>Create New Backend</h2>
        <form id="create-backend-form">
            <div class="form-group">
                <label>Backend Name *</label>
                <input type="text" name="name" required>
            </div>
            <div class="form-group">
                <label>Backend Type *</label>
                <select name="type" onchange="updateBackendFields(this.value)">
                    <option value="local">Local Filesystem</option>
                    <option value="s3">AWS S3 / S3-Compatible</option>
                    <option value="gcs">Google Cloud Storage</option>
                    <option value="gdrive">Google Drive</option>
                    <option value="azure">Azure Blob Storage</option>
                    <option value="b2">Backblaze B2</option>
                </select>
            </div>
            <div id="backend-config-fields">
                ${getBackendConfigFields('local', {})}
            </div>
            <div class="form-actions">
                <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                <button type="submit" class="btn btn-primary">Create Backend</button>
            </div>
        </form>
    `);

    document.getElementById('create-backend-form').addEventListener('submit', handleCreateBackend);
    showModal();
}

async function handleCreateBackend(e) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    // Collect all config fields
    const config = {};
    for (let [key, value] of formData.entries()) {
        if (key.startsWith('config_')) {
            const configKey = key.replace('config_', '');
            if (value && value.trim() !== '') {
                config[configKey] = value;
            }
        }
    }

    const backend = {
        name: formData.get('name'),
        type: formData.get('type'),
        config: config,
        enabled: true
    };

    try {
        const response = await backendsAPI.create(backend);

        if (response.success) {
            closeModal();
            loadBackends();
            showToast('Backend created successfully', 'success');
        } else {
            showToast('Failed to create backend: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function editBackend(backendId) {
    try {
        const response = await backendsAPI.get(backendId);

        if (!response.success) {
            showToast('Failed to load backend', 'error');
            return;
        }

        const backend = response.data;

        setModalContent(`
            <h2>Edit Backend</h2>
            <form id="edit-backend-form">
                <div class="form-group">
                    <label>Backend Name *</label>
                    <input type="text" name="name" value="${backend.name}" required>
                </div>
                <div class="form-group">
                    <label>Backend Type *</label>
                    <select name="type" disabled style="background: #1a1a1a;">
                        <option value="${backend.type}" selected>${backend.type.toUpperCase()}</option>
                    </select>
                    <small style="color: #666;">Type cannot be changed</small>
                </div>
                <div id="backend-config-fields">
                    ${getBackendConfigFields(backend.type, backend.config)}
                </div>
                <div class="form-group">
                    <label>Status</label>
                    <select name="enabled">
                        <option value="true" ${backend.enabled ? 'selected' : ''}>Enabled</option>
                        <option value="false" ${!backend.enabled ? 'selected' : ''}>Disabled</option>
                    </select>
                </div>
                <div class="form-actions">
                    <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                    <button type="submit" class="btn btn-primary">Save Changes</button>
                </div>
            </form>
        `);

        document.getElementById('edit-backend-form').addEventListener('submit', (e) => handleEditBackend(e, backendId, backend.type));
        showModal();
    } catch (error) {
        showToast('Error loading backend: ' + error.message, 'error');
    }
}

async function handleEditBackend(e, backendId, backendType) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    // Collect all config fields
    const config = {};
    for (let [key, value] of formData.entries()) {
        if (key.startsWith('config_')) {
            const configKey = key.replace('config_', '');
            if (value && value.trim() !== '') {
                config[configKey] = value;
            }
        }
    }

    const backend = {
        name: formData.get('name'),
        type: backendType,
        config: config,
        enabled: formData.get('enabled') === 'true'
    };

    try {
        const response = await backendsAPI.update(backendId, backend);

        if (response.success) {
            closeModal();
            loadBackends();
            showToast('Backend updated successfully', 'success');
        } else {
            showToast('Failed to update backend: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function deleteBackend(backendId) {
    if (!confirm('Are you sure you want to delete this backend?')) {
        return;
    }

    try {
        const response = await backendsAPI.delete(backendId);

        if (response.success) {
            loadBackends();
            showToast('Backend deleted successfully', 'success');
        } else {
            showToast('Failed to delete backend: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}
