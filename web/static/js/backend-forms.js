// Backend configuration forms module

export function getBackendConfigFields(backendType, config = {}) {
    switch(backendType) {
        case 'local':
            return `
                <div class="form-group">
                    <label>Storage Path *</label>
                    <input type="text" name="config_path" value="${config.path || ''}" placeholder="backups/local" required>
                    <small style="color: #666;">Relative to root directory or absolute path</small>
                </div>
            `;

        case 's3':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Region *</label>
                    <input type="text" name="config_region" value="${config.region || 'us-east-1'}" placeholder="us-east-1" required>
                </div>
                <div class="form-group">
                    <label>Access Key ID</label>
                    <input type="text" name="config_access_key_id" value="${config.access_key_id || ''}" placeholder="Leave empty to use IAM role">
                    <small style="color: #666;">Optional: Uses IAM role if not provided</small>
                </div>
                <div class="form-group">
                    <label>Secret Access Key</label>
                    <input type="password" name="config_secret_access_key" value="${config.secret_access_key || ''}" placeholder="Leave empty to use IAM role">
                    <small style="color: #666;">Optional: Uses IAM role if not provided</small>
                </div>
                <div class="form-group">
                    <label>Custom Endpoint (for S3-compatible storage)</label>
                    <input type="text" name="config_endpoint" value="${config.endpoint || ''}" placeholder="https://minio.example.com:9000">
                    <small style="color: #666;">Optional: For MinIO, DigitalOcean Spaces, etc.</small>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
                <div class="form-group">
                    <label>Storage Tier (Class)</label>
                    <select name="config_storage_tier">
                        <option value="STANDARD" ${(config.storage_tier || 'STANDARD') === 'STANDARD' ? 'selected' : ''}>Standard (default, frequent access)</option>
                        <option value="STANDARD_IA" ${config.storage_tier === 'STANDARD_IA' ? 'selected' : ''}>Standard-IA (infrequent access, lower cost)</option>
                        <option value="ONEZONE_IA" ${config.storage_tier === 'ONEZONE_IA' ? 'selected' : ''}>One Zone-IA (single AZ, lowest IA cost)</option>
                        <option value="INTELLIGENT_TIERING" ${config.storage_tier === 'INTELLIGENT_TIERING' ? 'selected' : ''}>Intelligent-Tiering (automatic optimization)</option>
                        <option value="GLACIER_IR" ${config.storage_tier === 'GLACIER_IR' ? 'selected' : ''}>Glacier Instant Retrieval (archival, instant)</option>
                        <option value="GLACIER" ${config.storage_tier === 'GLACIER' ? 'selected' : ''}>Glacier Flexible Retrieval (3-5hr retrieval)</option>
                        <option value="DEEP_ARCHIVE" ${config.storage_tier === 'DEEP_ARCHIVE' ? 'selected' : ''}>Glacier Deep Archive (12hr retrieval, cheapest)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval fees/delays.</small>
                </div>
            `;

        case 'gcs':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Credentials File Path</label>
                    <input type="text" name="config_credentials_file" value="${config.credentials_file || ''}" placeholder="config/gcs-service-account.json">
                    <small style="color: #666;">Path to service account JSON key (relative to root)</small>
                </div>
                <div class="form-group">
                    <label>OR Credentials JSON</label>
                    <textarea name="config_credentials_json" rows="4" placeholder='{"type":"service_account",...}'>${config.credentials_json || ''}</textarea>
                    <small style="color: #666;">Paste service account JSON directly</small>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
                <div class="form-group">
                    <label>Storage Class</label>
                    <select name="config_storage_tier">
                        <option value="STANDARD" ${(config.storage_tier || 'STANDARD') === 'STANDARD' ? 'selected' : ''}>Standard (default, frequent access)</option>
                        <option value="NEARLINE" ${config.storage_tier === 'NEARLINE' ? 'selected' : ''}>Nearline (30-day minimum, lower cost)</option>
                        <option value="COLDLINE" ${config.storage_tier === 'COLDLINE' ? 'selected' : ''}>Coldline (90-day minimum, very low cost)</option>
                        <option value="ARCHIVE" ${config.storage_tier === 'ARCHIVE' ? 'selected' : ''}>Archive (365-day minimum, cheapest)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval fees.</small>
                </div>
            `;

        case 'azure':
            return `
                <div class="form-group">
                    <label>Container Name *</label>
                    <input type="text" name="config_container" value="${config.container || ''}" placeholder="backups" required>
                </div>
                <div class="form-group">
                    <label>Account Name *</label>
                    <input type="text" name="config_account_name" value="${config.account_name || ''}" placeholder="mystorageaccount" required>
                </div>
                <div class="form-group">
                    <label>Authentication Method *</label>
                    <select name="auth_method" onchange="updateAzureAuthFields(this.value)">
                        <option value="account_key" ${config.account_key ? 'selected' : ''}>Account Key</option>
                        <option value="sas_token" ${config.sas_token ? 'selected' : ''}>SAS Token</option>
                        <option value="connection_string" ${config.connection_string ? 'selected' : ''}>Connection String</option>
                    </select>
                </div>
                <div id="azure-auth-fields">
                    <div class="form-group" id="azure-account-key">
                        <label>Account Key</label>
                        <input type="password" name="config_account_key" value="${config.account_key || ''}" placeholder="Account key">
                    </div>
                    <div class="form-group" id="azure-sas-token" style="display: none;">
                        <label>SAS Token</label>
                        <input type="text" name="config_sas_token" value="${config.sas_token || ''}" placeholder="sv=2020-08-04&ss=b...">
                    </div>
                    <div class="form-group" id="azure-connection-string" style="display: none;">
                        <label>Connection String</label>
                        <textarea name="config_connection_string" rows="3" placeholder="DefaultEndpointsProtocol=https;AccountName=...">${config.connection_string || ''}</textarea>
                    </div>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within container</small>
                </div>
                <div class="form-group">
                    <label>Access Tier</label>
                    <select name="config_storage_tier">
                        <option value="" ${!config.storage_tier ? 'selected' : ''}>Account Default</option>
                        <option value="Hot" ${config.storage_tier === 'Hot' ? 'selected' : ''}>Hot (frequent access, highest cost)</option>
                        <option value="Cool" ${config.storage_tier === 'Cool' ? 'selected' : ''}>Cool (30-day minimum, lower cost)</option>
                        <option value="Cold" ${config.storage_tier === 'Cold' ? 'selected' : ''}>Cold (90-day minimum, very low cost)</option>
                        <option value="Archive" ${config.storage_tier === 'Archive' ? 'selected' : ''}>Archive (180-day minimum, cheapest, rehydration required)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval delays.</small>
                </div>
            `;

        case 'b2':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Application Key ID *</label>
                    <input type="text" name="config_key_id" value="${config.key_id || ''}" placeholder="Key ID" required>
                </div>
                <div class="form-group">
                    <label>Application Key *</label>
                    <input type="password" name="config_application_key" value="${config.application_key || ''}" placeholder="Application key" required>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
            `;

        case 'gdrive':
            return `
                <div class="form-group">
                    <label>Credentials File Path</label>
                    <input type="text" name="config_credentials_file" value="${config.credentials_file || ''}" placeholder="config/gdrive-service-account.json">
                    <small style="color: #666;">Path to service account JSON key (relative to root)</small>
                </div>
                <div class="form-group">
                    <label>OR Credentials JSON</label>
                    <textarea name="config_credentials_json" rows="4" placeholder='{"type":"service_account",...}'>${config.credentials_json || ''}</textarea>
                    <small style="color: #666;">Paste service account JSON directly</small>
                </div>
                <div class="form-group">
                    <label>Folder Name</label>
                    <input type="text" name="config_folder_name" value="${config.folder_name || 'archivist-backups'}" placeholder="archivist-backups">
                    <small style="color: #666;">Name of folder to create/use in Drive</small>
                </div>
                <div class="form-group">
                    <label>OR Specific Folder ID</label>
                    <input type="text" name="config_folder_id" value="${config.folder_id || ''}" placeholder="1aBcDeFgHiJkLmNoPqRsTuVwXyZ">
                    <small style="color: #666;">Use specific folder ID (overrides folder name)</small>
                </div>
            `;

        default:
            return '<p>Unknown backend type</p>';
    }
}

// Expose globally for onclick handlers
window.updateAzureAuthFields = function(method) {
    document.getElementById('azure-account-key').style.display = method === 'account_key' ? 'block' : 'none';
    document.getElementById('azure-sas-token').style.display = method === 'sas_token' ? 'block' : 'none';
    document.getElementById('azure-connection-string').style.display = method === 'connection_string' ? 'block' : 'none';
};

window.updateBackendFields = function(backendType) {
    const container = document.getElementById('backend-config-fields');
    container.innerHTML = getBackendConfigFields(backendType, {});
};
