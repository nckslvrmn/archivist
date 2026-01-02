// Form builder module

export function createFormGroup(label, content, helpText = '') {
    return `
        <div class="form-group">
            <label>${label}</label>
            ${content}
            ${helpText ? `<small style="color: #666;">${helpText}</small>` : ''}
        </div>
    `;
}

export function createInput(name, value = '', placeholder = '', type = 'text', required = false) {
    return `<input type="${type}" name="${name}" value="${value}" placeholder="${placeholder}" ${required ? 'required' : ''}>`;
}

export function createTextarea(name, value = '', rows = 3) {
    return `<textarea name="${name}" rows="${rows}">${value}</textarea>`;
}

export function createSelect(name, options, selected = '', onChange = '') {
    const optionsHTML = Array.isArray(options)
        ? options.map(opt => {
            const optValue = typeof opt === 'string' ? opt : opt.value;
            const optLabel = typeof opt === 'string' ? opt : opt.label;
            const isSelected = optValue === selected ? 'selected' : '';
            return `<option value="${optValue}" ${isSelected}>${optLabel}</option>`;
        }).join('')
        : options;

    return `<select name="${name}" ${onChange ? `onchange="${onChange}"` : ''}>${optionsHTML}</select>`;
}

export function createCheckboxGroup(name, items, checkedIds = []) {
    // Check if items have 'type' property (backend items) for special styling
    const isBackendSelector = items.length > 0 && items[0].type;

    if (isBackendSelector) {
        return items.map(item => {
            const itemId = item.id || item.value;
            const itemName = item.name || item.label;
            const itemType = item.type ? item.type.toUpperCase() : '';
            const isChecked = checkedIds.includes(itemId) ? 'checked' : '';
            return `
                <label class="backend-option">
                    <input type="checkbox" name="${name}" value="${itemId}" ${isChecked}>
                    <span class="backend-option-content">
                        <span class="backend-option-name">${itemName}</span>
                        <span class="backend-option-type">${itemType}</span>
                    </span>
                </label>
            `;
        }).join('');
    } else {
        return items.map(item => {
            const itemId = item.id || item.value;
            const itemLabel = item.name || item.label;
            const isChecked = checkedIds.includes(itemId) ? 'checked' : '';
            return `
                <div class="checkbox-item">
                    <label>
                        <input type="checkbox" name="${name}" value="${itemId}" ${isChecked}>
                        ${itemLabel}
                    </label>
                </div>
            `;
        }).join('');
    }
}

export function createFormActions(cancelText = 'Cancel', submitText = 'Submit') {
    return `
        <div class="form-actions">
            <button type="button" class="btn" onclick="closeModal()">${cancelText}</button>
            <button type="submit" class="btn btn-primary">${submitText}</button>
        </div>
    `;
}
