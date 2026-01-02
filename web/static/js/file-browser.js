// File browser module
import { sourcesAPI } from './api.js';
import { formatBytes, showToast } from './utils.js';
import { showModal, closeModal, setModalContent } from './modal.js';

let currentBrowsePath = '';

export async function showFileBrowser(inputId) {
    const input = document.getElementById(inputId);

    const template = document.getElementById('file-browser-modal-template');
    const content = template.content.cloneNode(true);

    const tempDiv = document.createElement('div');
    tempDiv.appendChild(content);
    setModalContent(tempDiv.innerHTML);

    showModal();

    // Set up select button handler
    document.getElementById('select-path-btn').addEventListener('click', () => {
        const settings = { sources_dir: '/data/sources' };
        const fullPath = currentBrowsePath
            ? `${settings.sources_dir}/${currentBrowsePath}`
            : settings.sources_dir;

        input.value = fullPath;
        closeModal();
        showToast('Source path selected: ' + fullPath, 'success');
    });

    await loadDirectoryContents('');
}

async function loadDirectoryContents(path) {
    currentBrowsePath = path;
    const browserList = document.getElementById('browser-list');
    const currentPathDisplay = document.getElementById('current-path');
    const upButton = document.getElementById('up-button');

    currentPathDisplay.textContent = path || '(root)';
    upButton.disabled = !path;

    try {
        const response = await sourcesAPI.list(path);

        if (!response.success) {
            browserList.innerHTML = '<p style="padding: 1rem; color: #ff4444;">Failed to load directory</p>';
            return;
        }

        const entries = response.data.entries || [];

        if (entries.length === 0) {
            browserList.innerHTML = '<p style="padding: 1rem; color: #888;">Empty directory</p>';
            return;
        }

        const sorted = entries.sort((a, b) => {
            if (a.type === 'directory' && b.type !== 'directory') return -1;
            if (a.type !== 'directory' && b.type === 'directory') return 1;
            return a.name.localeCompare(b.name);
        });

        const template = document.getElementById('file-browser-entry-template');
        browserList.innerHTML = '';

        sorted.forEach(entry => {
            const entryElem = template.content.cloneNode(true);
            const isDir = entry.type === 'directory';
            const icon = isDir ? '📁' : '📄';
            const sizeInfo = isDir ? `${entry.file_count || 0} files` : formatBytes(entry.size);

            // Fill in fields
            entryElem.querySelector('[data-field="icon"]').textContent = icon;

            const nameElem = entryElem.querySelector('[data-field="name"]');
            nameElem.textContent = entry.name;
            nameElem.style.fontWeight = isDir ? 'bold' : 'normal';

            entryElem.querySelector('[data-field="size-info"]').textContent = sizeInfo;

            // Add click handler and hover effect for directories
            const browserEntry = entryElem.querySelector('.browser-entry');
            if (isDir) {
                browserEntry.style.cursor = 'pointer';
                browserEntry.onclick = () => navigateInto(entry.name);
                browserEntry.onmouseover = () => browserEntry.style.background = '#2a2a2a';
                browserEntry.onmouseout = () => browserEntry.style.background = '';
            }

            browserList.appendChild(entryElem);
        });
    } catch (error) {
        browserList.innerHTML = '<p style="padding: 1rem; color: #ff4444;">Error loading directory</p>';
        console.error('Failed to load directory:', error);
    }
}

// Expose functions globally
window.navigateInto = function(dirName) {
    const newPath = currentBrowsePath ? `${currentBrowsePath}/${dirName}` : dirName;
    loadDirectoryContents(newPath);
};

window.navigateUp = function() {
    if (!currentBrowsePath) return;
    const parts = currentBrowsePath.split('/');
    parts.pop();
    const newPath = parts.join('/');
    loadDirectoryContents(newPath);
};

window.showFileBrowser = showFileBrowser;
