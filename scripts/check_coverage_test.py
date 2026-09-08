import io
import unittest

from check_coverage import check, package_coverage


class CoverageTests(unittest.TestCase):
    def test_cross_package_execution_is_merged(self):
        profile = "mode: atomic\napp/a.go:1.1,2.1 5 0\napp/a.go:1.1,2.1 5 1\n"
        self.assertEqual({"app": [5, 5]}, package_coverage(profile))
        self.assertTrue(check(profile, io.StringIO()))

    def test_large_package_cannot_hide_small_package(self):
        profile = "mode: count\nbig/a.go:1.1,2.1 1000 1\nsmall/a.go:1.1,2.1 1 0\n"
        self.assertFalse(check(profile, io.StringIO()))

    def test_strict_threshold_uses_unrounded_counts(self):
        for covered, expected in [(95, False), (96, True)]:
            profile = f"mode: atomic\napp/a.go:1.1,2.1 {covered} 1\napp/b.go:1.1,2.1 {100-covered} 0\n"
            self.assertEqual(expected, check(profile, io.StringIO()))

    def test_invalid_profiles_fail_closed(self):
        for profile in ["", "mode: atomic\n", "mode: unknown\n", "mode: atomic\ninvalid\n", "mode: atomic\napp/a.go:1.1,2.1 -1 0\n", "mode: atomic\napp/a.go:1.1,2.1 1 0\napp/a.go:1.1,2.1 2 1\n"]:
            with self.subTest(profile=profile), self.assertRaises(ValueError):
                package_coverage(profile)
