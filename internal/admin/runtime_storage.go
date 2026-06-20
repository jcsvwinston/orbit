package admin

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

const adminStorageBrowseRoot = "uploads"

func normalizeStorageBrowsePath(raw string) (string, error) {
	browsePath := strings.TrimSpace(raw)
	if browsePath == "" || browsePath == "/" {
		return adminStorageBrowseRoot, nil
	}

	normalized := path.Clean("/" + strings.ReplaceAll(browsePath, "\\", "/"))
	normalized = strings.TrimPrefix(normalized, "/")
	if normalized == "." || normalized == "" {
		return adminStorageBrowseRoot, nil
	}
	if normalized != adminStorageBrowseRoot && !strings.HasPrefix(normalized, adminStorageBrowseRoot+"/") {
		return "", fmt.Errorf("access denied: path outside storage root")
	}
	return normalized, nil
}

func listConfiguredStorage(ctx context.Context, store storage.Store, browsePath string) ([]storageFileInfo, error) {
	prefix := browsePath
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	result, err := store.List(ctx, storage.ListOptions{
		Prefix:    prefix,
		Delimiter: "/",
		Limit:     1000,
	})
	if err != nil {
		return nil, err
	}

	files := make([]storageFileInfo, 0, len(result.Objects)+len(result.CommonPrefixes))
	for _, dir := range result.CommonPrefixes {
		dirPath := strings.TrimSuffix(dir, "/")
		files = append(files, storageFileInfo{
			Name:  path.Base(dirPath),
			Path:  dirPath,
			IsDir: true,
		})
	}
	for _, object := range result.Objects {
		objectPath := strings.TrimSuffix(object.Key, "/")
		files = append(files, storageFileInfo{
			Name:    path.Base(objectPath),
			Path:    objectPath,
			Size:    object.Size,
			IsDir:   false,
			ModTime: object.UpdatedAt,
		})
	}

	sortStorageEntries(files)
	return files, nil
}

func sortStorageEntries(files []storageFileInfo) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})
}
