// Dry run results rendering module
import { formatBytes } from './utils.js';

const formatNumber = (num) => num.toLocaleString();

function createSection(title, icon, content) {
    return `
        <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
            <h3 style="margin: 0 0 1rem 0; color: #00ff88;">${icon} ${title}</h3>
            ${content}
        </div>
    `;
}

function createStatsGrid(stats, columns = 'repeat(auto-fit, minmax(200px, 1fr))') {
    const container = document.createElement('div');
    container.style.cssText = `display: grid; grid-template-columns: ${columns}; gap: 1rem;`;

    stats.forEach(({ label, value, color }) => {
        const template = document.getElementById('dry-run-stat-card-template');
        const card = template.content.cloneNode(true);

        card.querySelector('[data-field="label"]').textContent = label;
        const valueElem = card.querySelector('[data-field="value"]');
        if (typeof value === 'object') {
            valueElem.innerHTML = value.html;
        } else {
            valueElem.textContent = value;
        }
        if (color) valueElem.style.color = color;

        container.appendChild(card);
    });

    return container.outerHTML;
}

function renderSummarySection(result) {
    const stats = [
        { label: 'Mode', value: result.mode === 'archive' ? '📦 Archive' : '🔄 Sync' },
        { label: 'Total Files', value: formatNumber(result.files_summary.total_files) },
        { label: 'Total Size', value: formatBytes(result.files_summary.total_size) },
        { label: 'Directories', value: formatNumber(result.files_summary.total_dirs) }
    ];

    return createSection('Summary', '📊', createStatsGrid(stats));
}

function renderArchiveDetails(archiveDetails) {
    const stats = [
        { label: 'Archive Name', value: archiveDetails.archive_name },
        { label: 'Estimated Size', value: formatBytes(archiveDetails.estimated_archive_size) },
        { label: 'Compression', value: `${Math.round((1 - archiveDetails.compression_ratio) * 100)}% reduction` }
    ];

    return createSection('Archive Details', '📦', createStatsGrid(stats));
}

function renderSyncDetails(syncDetails) {
    const stats = [
        {
            label: 'To Upload',
            value: {
                html: `${syncDetails.upload_count} files<div style="font-size: 0.85rem; color: #888;">${formatBytes(syncDetails.bytes_to_upload)}</div>`
            },
            color: '#00ff88'
        },
        { label: 'Unchanged', value: `${syncDetails.skip_count} files` },
        { label: 'To Delete', value: `${syncDetails.delete_count} files`, color: '#ff6b6b' }
    ];

    return createSection('Sync Analysis', '🔄', createStatsGrid(stats, 'repeat(auto-fit, minmax(150px, 1fr))'));
}

function renderBackends(backendPlans) {
    const container = document.createElement('div');
    container.style.cssText = 'display: grid; gap: 0.75rem;';

    backendPlans.forEach(backend => {
        const template = document.getElementById('dry-run-backend-template');
        const backendElem = template.content.cloneNode(true);

        const statusIcon = backend.available ? '✅' : '❌';
        const statusColor = backend.available ? '#00ff88' : '#ff6b6b';

        const card = backendElem.querySelector('div');
        card.style.setProperty('--status-color', statusColor);

        backendElem.querySelector('[data-field="name"]').textContent = `${statusIcon} ${backend.backend_name}`;
        backendElem.querySelector('[data-field="type"]').textContent = backend.backend_type;

        const pathElem = backendElem.querySelector('[data-field="path"]');
        if (backend.remote_path) {
            pathElem.textContent = `Path: ${backend.remote_path}`;
        } else {
            pathElem.remove();
        }

        const errorElem = backendElem.querySelector('[data-field="error"]');
        if (backend.error_message) {
            errorElem.textContent = backend.error_message;
        } else {
            errorElem.remove();
        }

        container.appendChild(backendElem);
    });

    return createSection('Backend Status', '🔌', container.outerHTML);
}

function renderTopFiles(topFiles) {
    const tbody = document.createElement('tbody');

    topFiles.forEach(file => {
        const template = document.getElementById('dry-run-file-row-template');
        const row = template.content.cloneNode(true);

        row.querySelector('[data-field="path"]').textContent = file.relative_path;
        row.querySelector('[data-field="size"]').textContent = formatBytes(file.size);

        tbody.appendChild(row);
    });

    const tableHTML = `
        <div style="max-height: 300px; overflow-y: auto;">
            <table style="width: 100%; border-collapse: collapse;">
                <thead>
                    <tr style="border-bottom: 1px solid #444;">
                        <th style="text-align: left; padding: 0.5rem; color: #888; font-size: 0.85rem;">File</th>
                        <th style="text-align: right; padding: 0.5rem; color: #888; font-size: 0.85rem;">Size</th>
                    </tr>
                </thead>
                ${tbody.outerHTML}
            </table>
        </div>
    `;

    return createSection('Largest Files (Top 10)', '📁', tableHTML);
}

function renderFileTypes(fileTypes) {
    const sortedTypes = Object.entries(fileTypes)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 10);

    const container = document.createElement('div');
    container.style.cssText = 'display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 0.75rem;';

    sortedTypes.forEach(([ext, count]) => {
        const template = document.getElementById('dry-run-filetype-template');
        const typeCard = template.content.cloneNode(true);

        typeCard.querySelector('[data-field="count"]').textContent = formatNumber(count);
        typeCard.querySelector('[data-field="extension"]').textContent = ext;

        container.appendChild(typeCard);
    });

    return `
        <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px;">
            <h3 style="margin: 0 0 1rem 0; color: #00ff88;">📊 File Types</h3>
            ${container.outerHTML}
        </div>
    `;
}

export function renderDryRunResults(result) {
    let sections = [];

    // Summary (always shown)
    sections.push(renderSummarySection(result));

    // Archive or Sync details
    if (result.archive_details) {
        sections.push(renderArchiveDetails(result.archive_details));
    }

    if (result.sync_details) {
        sections.push(renderSyncDetails(result.sync_details));
    }

    // Backend status
    sections.push(renderBackends(result.backend_plans));

    // Top files
    if (result.files_summary.top_files && result.files_summary.top_files.length > 0) {
        sections.push(renderTopFiles(result.files_summary.top_files));
    }

    // File types
    if (result.files_summary.file_types && Object.keys(result.files_summary.file_types).length > 0) {
        sections.push(renderFileTypes(result.files_summary.file_types));
    }

    return `
        <h2 style="margin: 0 0 1.5rem 0;">Dry Run Results - ${result.task_name}</h2>
        ${sections.join('')}
    `;
}
