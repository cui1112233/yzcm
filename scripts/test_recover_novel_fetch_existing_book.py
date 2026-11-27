import importlib.util
import pathlib
import unittest

path=pathlib.Path(__file__).with_name('recover-novel-fetch-existing-book.py')
spec=importlib.util.spec_from_file_location('recovery',path)
recovery=importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)

class RecoveryDocumentTests(unittest.TestCase):
    def test_recovered_document_keeps_results_but_cannot_auto_generate_or_submit(self):
        refs={v:{'versionId':v,'state':'ready','charCount':n} for v,n in [('original_raw',5656),('original',3734),('ai1',3733)]}
        doc=recovery.build_document(refs,'2026-11-27T04:00:00Z','backup-hash')
        self.assertEqual(doc['meta']['manualOnly'],True)
        self.assertEqual(doc['meta']['status'],'pending_submit')
        self.assertEqual(doc['meta']['siteSubmitStatus'],'pending_upload')
        self.assertEqual(doc['originalRawChars'],5656)
        self.assertEqual(doc['processedChars'],3734)
        self.assertEqual(doc['bodyRefs'],refs)
        self.assertEqual(doc['versions']['ai1']['state'],'ready')
        self.assertTrue(doc['meta']['originalTaskDateUnknown'])
        self.assertNotIn('gender',doc)
        self.assertNotIn('submission_id',str(doc))
        self.assertNotIn('OriginalRaw',doc)

    def test_incomplete_backup_cannot_be_promoted_to_restored_task(self):
        with self.assertRaises(ValueError):
            recovery.build_document({'original_raw':{'charCount':5656}},'2026-11-27T04:00:00Z','backup-hash')

if __name__=='__main__': unittest.main()
