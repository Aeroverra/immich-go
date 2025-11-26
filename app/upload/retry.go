package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (uc *UpCmd) retryFailedTags(ctx context.Context) (bool, error) {
	if _, err := os.Stat("failed_tags.json"); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if uc.NoUI {
		return uc.retryFailedTagsAction(ctx)
	}
	return uc.retryFailedTagsUI(ctx)
}

func (uc *UpCmd) retryFailedTagsAction(ctx context.Context) (bool, error) {
	f, err := os.Open("failed_tags.json")
	if err != nil {
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

func (uc *UpCmd) retryFailedTagsUI(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	uiApp := tview.NewApplication()
	ui := uc.newUI(ctx, uc.app)
	pages := tview.NewPages()
	uiApp.SetRoot(pages, true)
	pages.AddPage("ui", ui.screen, true, true)

	stopUI := func(err error) {
		cancel(err)
		if uiApp != nil {
			uiApp.Stop()
		}
	}

	// handle Ctrl+C and Ctrl+Q
	uiApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlQ, tcell.KeyCtrlC:
			ui.restoreLogger(uc.app)
			cancel(errors.New("interrupted: Ctrl+C or Ctrl+Q pressed"))
		case tcell.KeyEnter:
			// Only allow exit if done? Or always?
			// runUI allows exit if uploadDone is true.
			// We can use a flag.
		}
		return event
	})

	go func() {
		_, err := uc.retryFailedTagsAction(ctx)
		if err != nil {
			stopUI(err)
			return
		}

		// Show modal
		uiApp.QueueUpdateDraw(func() {
			messages := strings.Builder{}
			// We might not have counts if we didn't use the file processor for tagging events
			// But we can just show the "safe to exit" message.
			
			// Check for errors in failedTags
			uc.failedTagsMu.Lock()
			failedCount := len(uc.failedTags)
			uc.failedTagsMu.Unlock()

			if failedCount > 0 {
				messages.WriteString(fmt.Sprintf("%d tags failed to apply. Check log for details.\n", failedCount))
			} else {
				messages.WriteString("All tags applied successfully.\n")
			}

			modal := newModal(messages.String())
			pages.AddPage("modal", modal, true, false)
			pages.ShowPage("modal")
			
			// Update input capture to allow exit on Enter
			uiApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				switch event.Key() {
				case tcell.KeyCtrlQ, tcell.KeyCtrlC:
					ui.restoreLogger(uc.app)
					cancel(errors.New("interrupted"))
				case tcell.KeyEnter:
					stopUI(nil)
				}
				return event
			})
		})
	}()

	if err := uiApp.Run(); err != nil {
		return false, err
	}

	return true, nil
}
