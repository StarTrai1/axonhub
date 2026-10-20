import unittest

from select_changes import select


class SelectionTests(unittest.TestCase):
    def test_documentation_does_not_build(self):
        self.assertFalse(any(select(["docs/en/guides/unified-conversation-history.md"]).values()))

    def test_frontend_does_not_repeat_backend_suites(self):
        result = select(["frontend/src/features/channels/index.tsx"])
        self.assertTrue(result["frontend"])
        self.assertFalse(result["backend"])

    def test_gateway_change_keeps_windows_and_protocol(self):
        result = select(["internal/server/orchestrator/unified_history_context.go"])
        for key in ("backend", "windows", "protocol"):
            self.assertTrue(result[key])
        self.assertFalse(result["generation"])

    def test_schema_and_dependency_changes_regenerate(self):
        self.assertTrue(select(["internal/ent/schema/channel.go"])["generation"])
        self.assertTrue(select(["go.mod"])["generation"])

    def test_workflow_unknown_and_full_ranges_do_not_skip(self):
        for paths, full in (([".github/workflows/test.yml"], False), (["new-owner/config.yaml"], False), ([], True)):
            self.assertTrue(all(select(paths, full).values()))


if __name__ == "__main__":
    unittest.main()
