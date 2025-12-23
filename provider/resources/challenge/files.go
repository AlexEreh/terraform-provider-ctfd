package challenge

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ctfer-io/go-ctfd/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/AlexEreh/terraform-provider-ctfd/provider/utils"
)

// CreateChallengeFiles uploads files from plan to CTFd and returns the updated list with IDs.
func CreateChallengeFiles(ctx context.Context, client *api.Client, challengeID int, filesFromPlan []FileSubresourceModel) ([]FileSubresourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	result := make([]FileSubresourceModel, 0, len(filesFromPlan))

	for _, fileModel := range filesFromPlan {
		// Read file content from disk
		if fileModel.Path.IsNull() || fileModel.Path.IsUnknown() {
			diags.AddError(
				"Invalid File Configuration",
				fmt.Sprintf("File '%s' must have a valid 'path' attribute for upload", fileModel.Name.ValueString()),
			)
			continue
		}

		filePath := fileModel.Path.ValueString()
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			diags.AddError(
				"File Read Error",
				fmt.Sprintf("Unable to read file at path '%s': %s", filePath, err),
			)
			continue
		}

		// Upload file to CTFd
		fileType := fileModel.Type
		if fileType.IsNull() || fileType.IsUnknown() {
			fileType = FileTypeChallenge
		}
		location := fileModel.Location
		if location.IsNull() || location.IsUnknown() {
			location = FileLocationChallenge
		}

		fileName := fileModel.Name.ValueString()
		uploadedFiles, err := client.PostFiles(&api.PostFilesParams{
			Files: []*api.InputFile{
				{
					Name:    fileName,
					Content: fileContent,
				},
			},
			Challenge: &challengeID,
			Location:  utils.Ptr(location.ValueString()),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
		if err != nil {
			diags.AddError(
				"Client Error",
				fmt.Sprintf("Unable to upload file '%s' for challenge %d: %s", fileName, challengeID, err),
			)
			continue
		}

		// CTFd API returns a list of uploaded files; we expect one file per call
		if len(uploadedFiles) == 0 {
			diags.AddError(
				"Unexpected API Response",
				fmt.Sprintf("No file returned after upload for '%s'", fileName),
			)
			continue
		}

		uploaded := uploadedFiles[0]

		// Build the result model with computed fields
		resultFile := FileSubresourceModel{
			ID:         types.Int64Value(int64(uploaded.ID)),
			Name:       types.StringValue(fileName),
			Path:       fileModel.Path,
			Type:       types.StringValue(uploaded.Type),
			Location:   types.StringValue(uploaded.Location),
			Challenge:  types.Int64Value(int64(challengeID)),
			URL:        types.StringValue(fmt.Sprintf("/files/%s", uploaded.Location)),
			AccessType: types.StringValue("public"), // Default value, CTFd doesn't return this
		}
		result = append(result, resultFile)
	}

	return result, diags
}

// ReadChallengeFiles retrieves file metadata from CTFd for a given challenge.
func ReadChallengeFiles(ctx context.Context, client *api.Client, challengeID int) ([]FileSubresourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Get files for the challenge from CTFd
	// Note: CTFd API doesn't provide a way to filter files by challenge_id directly,
	// but we can try to get files and filter manually if needed.
	// For now, we'll assume GetChallengeFiles method exists or use a workaround.
	files, err := client.GetFiles(&api.GetFilesParams{
		Type:     utils.Ptr("challenge"),
		Location: utils.Ptr("challenge"),
	}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		diags.AddError(
			"Client Error",
			fmt.Sprintf("Unable to read files for challenge %d: %s", challengeID, err),
		)
		return nil, diags
	}

	result := make([]FileSubresourceModel, 0)
	for _, file := range files {
		// Note: The CTFd File struct doesn't have ChallengeID field according to the model.go
		// We may need to rely on the file location or other means to associate with challenge
		// For now, we'll extract the filename from the location
		fileName := extractFileName(file.Location)

		result = append(result, FileSubresourceModel{
			ID:         types.Int64Value(int64(file.ID)),
			Name:       types.StringValue(fileName),
			Path:       types.StringNull(), // We cannot read back the original path
			Type:       types.StringValue(file.Type),
			Location:   types.StringValue(file.Location),
			Challenge:  types.Int64Value(int64(challengeID)),
			URL:        types.StringValue(fmt.Sprintf("/files/%s", file.Location)),
			AccessType: types.StringValue("public"), // Default, not provided by API
		})
	}

	return result, diags
}

// extractFileName extracts the filename from a file location path
func extractFileName(location string) string {
	// Location is typically something like "abc123/filename.txt"
	// We want to extract just the filename
	parts := strings.Split(location, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return location
}

// SyncChallengeFilesOnUpdate handles file updates by deleting removed files and uploading new ones.
func SyncChallengeFilesOnUpdate(ctx context.Context, client *api.Client, challengeID int, oldFiles, newFiles []FileSubresourceModel) ([]FileSubresourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Build maps for comparison (by name, as a logical key)
	oldByName := make(map[string]FileSubresourceModel)
	for _, f := range oldFiles {
		oldByName[f.Name.ValueString()] = f
	}

	newByName := make(map[string]FileSubresourceModel)
	for _, f := range newFiles {
		newByName[f.Name.ValueString()] = f
	}

	// Delete files that are no longer in the new config
	for name, oldFile := range oldByName {
		if _, exists := newByName[name]; !exists {
			// File removed, delete it
			if !oldFile.ID.IsNull() {
				if err := client.DeleteFile(strconv.Itoa(int(oldFile.ID.ValueInt64())), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
					diags.AddWarning(
						"File Delete Warning",
						fmt.Sprintf("Unable to delete file '%s' (ID: %d): %s", name, oldFile.ID.ValueInt64(), err),
					)
				}
			}
		}
	}

	// Upload new files (files that don't have an ID or have changed path)
	result := make([]FileSubresourceModel, 0, len(newFiles))
	for _, newFile := range newFiles {
		oldFile, existedBefore := oldByName[newFile.Name.ValueString()]

		// If the file existed and has the same path, keep it
		if existedBefore && !oldFile.ID.IsNull() {
			// Check if path changed (if path is specified in new config)
			if !newFile.Path.IsNull() && !oldFile.Path.IsNull() && newFile.Path.Equal(oldFile.Path) {
				// Path unchanged, reuse old file
				result = append(result, oldFile)
				continue
			}

			// Path changed or new path specified: delete old, upload new
			if err := client.DeleteFile(strconv.Itoa(int(oldFile.ID.ValueInt64())), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
				diags.AddWarning(
					"File Delete Warning",
					fmt.Sprintf("Unable to delete old version of file '%s' (ID: %d): %s", newFile.Name.ValueString(), oldFile.ID.ValueInt64(), err),
				)
			}
		}

		// Upload the new file
		uploaded, uploadDiags := CreateChallengeFiles(ctx, client, challengeID, []FileSubresourceModel{newFile})
		diags.Append(uploadDiags...)
		if len(uploaded) > 0 {
			result = append(result, uploaded[0])
		}
	}

	return result, diags
}
