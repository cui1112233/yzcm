package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"qiantie/backend/internal/batchfactoryv11"

	"qiantie/backend/internal/novelfetchworkshop"
)

type workshopClassificationProvider func(context.Context, novelfetchworkshop.RewriteRequest) (string, error)

func (p workshopClassificationProvider) Complete(ctx context.Context, req novelfetchworkshop.RewriteRequest) (string, error) {
	return p(ctx, req)
}

// Register before spending, then commit the pair atomically. A lost result-save
// acknowledgement must never turn a reclaimed delivery into another AI call.
func runWorkshopClassification(ctx context.Context, store novelfetchworkshop.Store, owner string, document *novelfetchworkshop.Document, info novelfetchworkshop.BookInfo, judge novelfetchworkshop.AIGenderJudge, provider novelfetchworkshop.RewriteProvider, preparedFetch ...bool) error {
	latest, err := store.GetDocument(ctx, owner, document.BookID)
	if err != nil {
		return err
	}
	if novelFetchDocumentStopped(latest) {
		return novelfetchworkshop.ErrBookCancelled
	}
	if inFlight, _ := latest.Meta["classificationInFlight"].(bool); inFlight {
		return fmt.Errorf("分类结果未知，请人工确认后手动重试")
	}
	// A failed classification is one attempt in this round. Explicit retry
	// changes the queue state before allowing another attempt; scanners and
	// reclaimed deliveries must not silently classify again.
	classificationRetry := novelFetchRetrySchedule(latest.Meta)[novelFetchRetryStageClassify]
	if !novelFetchClassificationResolved(latest) && (firstText(latest.Meta["status"]) == "waiting_classification" || intValue(classificationRetry["attempt"]) > 0 || novelFetchRetryStateIsManual(classificationRetry)) {
		return errNovelFetchClassificationPending
	}
	ctx = batchfactoryv11.WithSingleAttempt(ctx)
	// Start from current user decisions and retry/configuration state. Only the
	// fetch caller may merge its newly prepared rule output, never stale labels.
	if latest.Meta == nil {
		latest.Meta = map[string]any{}
	}
	if len(preparedFetch) > 0 && preparedFetch[0] {
		for _, key := range []string{"originalStatus", "originalRawChars", "processedChars", "sensitiveStatus", "sensitiveReplaced", "rulesTrace", "rulesTraceByVersion", "sensitiveHits", "sensitiveFixed"} {
			if value, ok := document.Meta[key]; ok {
				latest.Meta[key] = value
			}
		}
		if latest.Meta["sensitiveStatus"] == "done" {
			clearNovelFetchTaskRetry(latest.Meta, novelFetchRetryStageSensitive)
		}
	}
	*document = latest
	priorClassificationError := firstText(document.Meta["classifyError"])
	needsGender := genderNeedsResolution(document.Gender)
	needsStyle := styleNeedsResolution(document.Meta)
	paid := false
	var reservationErr error
	reserve := func() error {
		if reservationErr != nil {
			return reservationErr
		}
		if paid {
			return nil
		}
		document.Meta["classificationInFlight"] = true
		version, e := store.CompareAndSwapDocument(ctx, owner, *document)
		if e != nil {
			reservationErr = e
			return e
		}
		document.UpdatedAt = version
		paid = true
		return nil
	}
	var guardedJudge novelfetchworkshop.AIGenderJudge
	if needsGender && judge != nil {
		guardedJudge = func(c context.Context, i novelfetchworkshop.BookInfo) (string, error) {
			if e := reserve(); e != nil {
				return "", e
			}
			return judge(c, i)
		}
	}
	var guardedProvider novelfetchworkshop.RewriteProvider
	if needsStyle && provider != nil {
		guardedProvider = workshopClassificationProvider(func(c context.Context, r novelfetchworkshop.RewriteRequest) (string, error) {
			if e := reserve(); e != nil {
				return "", e
			}
			return provider.Complete(c, r)
		})
	}
	if needsGender {
		d := novelfetchworkshop.ResolveGender(ctx, "", info, guardedJudge)
		if reservationErr != nil {
			return reservationErr
		}
		document.Gender, document.GenderSource, document.GenderReason, document.GenderConfidence = d.Gender, d.Source, d.Reason, d.Confidence
	}
	if needsStyle {
		d := novelfetchworkshop.ResolveStyle(ctx, info, guardedProvider)
		if reservationErr != nil {
			return reservationErr
		}
		recordNovelFetchStyleDecision(document.Meta, d, nil)
	}

	syncNovelFetchClassificationMetadata(document)
	resolved := novelFetchClassificationResolved(*document)
	if resolved {
		delete(document.Meta, "classifyError")
		if priorClassificationError != "" && firstText(document.Meta["error"]) == priorClassificationError {
			delete(document.Meta, "error")
		}
		clearNovelFetchTaskRetry(document.Meta, novelFetchRetryStageClassify)
	} else {
		reasons := []string{}
		if genderNeedsResolution(document.Gender) {
			reasons = append(reasons, document.GenderReason)
		}
		if styleNeedsResolution(document.Meta) {
			reasons = append(reasons, firstText(document.Meta["styleError"]))
		}
		document.Meta["classifyError"] = strings.Join(reasons, "；")
		document.Meta["status"], document.Meta["queueState"] = "waiting_classification", "waiting_classification"
		document.Meta["error"] = firstText(document.Meta["classifyError"], errNovelFetchClassificationPending.Error())
		scheduleNovelFetchTaskRetry(document.Meta, novelFetchRetryStageClassify, fmt.Errorf("%s", document.Meta["error"]), 0, time.Now().UTC())
	}
	delete(document.Meta, "classificationInFlight")
	version, err := store.CompareAndSwapDocument(ctx, owner, *document)
	if err != nil {
		if paid {
			document.Meta["classificationInFlight"] = true
			return fmt.Errorf("分类结果未知，结果保存未确认，请人工检查: %w", err)
		}
		return err
	}
	document.UpdatedAt = version
	if !resolved {
		return errNovelFetchClassificationPending
	}
	return nil
}
