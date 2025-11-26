package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func (uc *UpCmd) retryFailedTags(ctx context.Context) (bool, error) {
	f, err := os.Open("failed_tags.json")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()

	uc.app.Log().Info("Found failed_tags.json, retrying failed tags...")

	var failedTags []FailedTag
	if err := json.NewDecoder(f).Decode(&failedTags); err != nil {
		return false, fmt.Errorf("failed to decode failed_tags.json: %w", err)
	}

	// Close the file so we can overwrite/delete it later
	f.Close()

	for _, ft := range failedTags {
		_, err := uc.saveTags(ctx, ft.Tag, ft.IDs)
		if err != nil {
			uc.app.Log().Error("Failed to retry tag", "tag", ft.Tag.Value, "err", err)
			// saveTags already adds to uc.failedTags if it fails, but here we are running in a special mode.
			// saveTags appends to uc.failedTags on error. We should probably clear uc.failedTags before starting or handle it differently.
			// Actually, saveTags appends to uc.failedTags. So if we just run saveTags, it will populate uc.failedTags with any *new* failures.
			// But wait, saveTags appends to uc.failedTags. If we are retrying, we should probably rely on that mechanism?
			// The issue is that saveTags appends. So if we iterate `failedTags` (local var) and call `saveTags`, `uc.failedTags` will get populated with failures.
			// So we can just check `uc.failedTags` at the end.
		}
	}

	// Check if there are any new failures
	uc.failedTagsMu.Lock()
	defer uc.failedTagsMu.Unlock()

	if len(uc.failedTags) == 0 {
		uc.app.Log().Info("All failed tags applied successfully.")
		if err := os.Remove("failed_tags.json"); err != nil {
			uc.app.Log().Error("Failed to delete failed_tags.json", "err", err)
		}
	} else {
		uc.app.Log().Warn(fmt.Sprintf("%d tags failed to apply during retry, updating failed_tags.json", len(uc.failedTags)))
		f, err := os.Create("failed_tags.json")
		if err != nil {
			uc.app.Log().Error("Failed to create failed_tags.json", "err", err)
		} else {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(uc.failedTags); err != nil {
				uc.app.Log().Error("Failed to write failed_tags.json", "err", err)
			}
			f.Close()
		}
	}

	return true, nil
}
