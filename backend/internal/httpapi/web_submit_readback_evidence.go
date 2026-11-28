package httpapi

import (
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/videomanagementconnector"
	"strings"
)

// An explicit accepted receipt is durable independently from later readback.
// Only the current dispatch attempt may provide this acknowledgement.
func workshopAcknowledgedPendingSubmission(doc novelfetchworkshop.Document, version string) (videomanagementconnector.Submission, bool) {
	attempt := mapOrEmpty(mapOrEmpty(doc.Meta["siteSubmitAttempts"])[version])
	operation := firstText(attempt["operation_id"])
	sourceBookID, _ := webSubmitSourceBookID(doc.Meta, doc.BookID)
	for i := len(doc.Logs) - 1; i >= 0; i-- {
		entry := mapOrEmpty(doc.Logs[i])
		if entry["event"] != "site_submit" || entry["version"] != version {
			continue
		}
		if operation != "" && firstText(entry["operation_id"]) != operation {
			continue
		}
		receipt := mapOrEmpty(entry["remote_receipt"])
		if receipt["accepted"] == true && entry["status"] == "submitted" {
			id := firstText(receipt["submission_id"])
			if pending := firstText(mapOrEmpty(doc.Meta["siteSubmitPendingConfirmation"])[version]); pending != "" && pending != doc.BookID && pending != sourceBookID && id != pending {
				continue
			}
			return videomanagementconnector.Submission{ID: id, Status: "submitted"}, true
		}
	}
	return videomanagementconnector.Submission{}, false
}

func workshopAcceptedReadbackMatches(ack, readback videomanagementconnector.Submission, doc novelfetchworkshop.Document, bookID, version string) bool {
	record := mapOrEmpty(readback.ReadbackRaw)
	if book := workshopRemoteRecordID(record["bookid"]); book != "" && book != bookID {
		return false
	}
	if strings.TrimSpace(readback.ID) == "" {
		return false
	}
	if ack.ID != "" {
		return readback.ID == ack.ID
	}
	// Without an acknowledged record ID a list must identify this book and
	// carry the existing operation/content/time evidence, never merely an old row.
	if workshopRemoteRecordID(record["bookid"]) != bookID {
		return false
	}
	return workshopPendingReadbackMatches(readback, doc, bookID, version)
}
func workshopHasUnacceptedPending(doc novelfetchworkshop.Document) bool {
	for version := range mapOrEmpty(doc.Meta["siteSubmitPendingConfirmation"]) {
		if _, accepted := workshopAcknowledgedPendingSubmission(doc, version); !accepted {
			return true
		}
	}
	return false
}
