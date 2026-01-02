package backend

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/nsilverman/archivist/internal/models"
)

// AzureBackend stores backups on Azure Blob Storage
type AzureBackend struct {
	client      *azblob.Client
	container   string
	prefix      string
	storageTier *blob.AccessTier
}

// Initialize sets up the Azure backend
func (b *AzureBackend) Initialize(cfg map[string]interface{}, pathResolver PathResolver) error {
	// Extract configuration
	containerName, ok := cfg["container"].(string)
	if !ok || containerName == "" {
		return fmt.Errorf("azure backend requires 'container' configuration")
	}
	b.container = containerName

	// Optional prefix
	if prefix, ok := cfg["prefix"].(string); ok {
		b.prefix = prefix
	}

	// Optional storage tier
	if tier, ok := cfg["storage_tier"].(string); ok && tier != "" {
		validTier, err := validateAzureAccessTier(tier)
		if err != nil {
			return err
		}
		b.storageTier = validTier
	}
	// If not specified, Azure will use the account's default tier

	// Get account name
	accountName, ok := cfg["account_name"].(string)
	if !ok || accountName == "" {
		return fmt.Errorf("azure backend requires 'account_name' configuration")
	}

	// Create client using account key or SAS token
	var client *azblob.Client
	var err error

	if accountKey, ok := cfg["account_key"].(string); ok && accountKey != "" {
		// Use account key authentication
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
		credential, credErr := azblob.NewSharedKeyCredential(accountName, accountKey)
		if credErr != nil {
			return fmt.Errorf("failed to create shared key credential: %w", credErr)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(serviceURL, credential, nil)
	} else if sasToken, ok := cfg["sas_token"].(string); ok && sasToken != "" {
		// Use SAS token authentication
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/?%s", accountName, sasToken)
		client, err = azblob.NewClientWithNoCredential(serviceURL, nil)
	} else if connectionString, ok := cfg["connection_string"].(string); ok && connectionString != "" {
		// Use connection string
		client, err = azblob.NewClientFromConnectionString(connectionString, nil)
	} else {
		return fmt.Errorf("azure backend requires one of: account_key, sas_token, or connection_string")
	}

	if err != nil {
		return fmt.Errorf("failed to create Azure client: %w", err)
	}

	b.client = client
	return nil
}

// Test checks if the backend is accessible
func (b *AzureBackend) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get container properties
	containerClient := b.client.ServiceClient().NewContainerClient(b.container)
	_, err := containerClient.GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("cannot access container: %w", err)
	}

	return nil
}

// Upload uploads a file to Azure Blob Storage
func (b *AzureBackend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
	}()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := stat.Size()

	// Add prefix if configured
	blobName := remotePath
	if b.prefix != "" {
		blobName = b.prefix + "/" + remotePath
	}

	// Wrap with progress reader
	progressReader := &progressReader{
		reader:   file,
		size:     fileSize,
		callback: progress,
	}

	// Configure upload options
	uploadOptions := &azblob.UploadStreamOptions{}
	if b.storageTier != nil {
		uploadOptions.AccessTier = b.storageTier
	}

	// Upload to blob
	_, err = b.client.UploadStream(ctx, b.container, blobName, progressReader, uploadOptions)
	if err != nil {
		return fmt.Errorf("failed to upload to Azure: %w", err)
	}

	return nil
}

// List returns all backups with a given prefix
func (b *AzureBackend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
	// Combine backend prefix with query prefix
	fullPrefix := prefix
	if b.prefix != "" {
		if prefix != "" {
			fullPrefix = b.prefix + "/" + prefix
		} else {
			fullPrefix = b.prefix
		}
	}

	var backups []BackupInfo
	containerClient := b.client.ServiceClient().NewContainerClient(b.container)

	pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &fullPrefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		for _, blob := range page.Segment.BlobItems {
			// Remove backend prefix from path for display
			displayPath := *blob.Name
			if b.prefix != "" && len(displayPath) > len(b.prefix)+1 {
				displayPath = displayPath[len(b.prefix)+1:]
			}

			backups = append(backups, BackupInfo{
				Path:         displayPath,
				Size:         *blob.Properties.ContentLength,
				LastModified: blob.Properties.LastModified.Format(time.RFC3339),
				Hash:         "", // Azure uses different hash format
			})
		}
	}

	return backups, nil
}

// Delete removes a backup file
func (b *AzureBackend) Delete(ctx context.Context, remotePath string) error {
	// Add prefix if configured
	blobName := remotePath
	if b.prefix != "" {
		blobName = b.prefix + "/" + remotePath
	}

	_, err := b.client.DeleteBlob(ctx, b.container, blobName, nil)
	if err != nil {
		return fmt.Errorf("failed to delete from Azure: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (b *AzureBackend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	// Calculate total size of blobs with our prefix
	var totalSize int64

	fullPrefix := b.prefix
	containerClient := b.client.ServiceClient().NewContainerClient(b.container)

	pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &fullPrefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate usage: %w", err)
		}

		for _, blob := range page.Segment.BlobItems {
			totalSize += *blob.Properties.ContentLength
		}
	}

	return &models.StorageUsage{
		Used:  totalSize,
		Total: -1, // Azure has no fixed limit
	}, nil
}

// Close closes the backend connection
func (b *AzureBackend) Close() error {
	// Azure client doesn't need explicit cleanup
	return nil
}

// validateAzureAccessTier validates and returns an Azure access tier
func validateAzureAccessTier(tier string) (*blob.AccessTier, error) {
	// Azure access tiers (case-insensitive)
	upperTier := strings.ToUpper(tier)

	switch upperTier {
	case "HOT":
		hot := blob.AccessTierHot
		return &hot, nil
	case "COOL":
		cool := blob.AccessTierCool
		return &cool, nil
	case "COLD":
		cold := blob.AccessTierCold
		return &cold, nil
	case "ARCHIVE":
		archive := blob.AccessTierArchive
		return &archive, nil
	default:
		return nil, fmt.Errorf("invalid Azure access tier: %s (valid options: Hot, Cool, Cold, Archive)", tier)
	}
}
