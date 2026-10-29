"""No subprocesses or GUI: helper contract and failure propagation."""

from pathlib import Path
import tempfile
import unittest

from go_profile_env import parser_env


class ParserEnvTests(unittest.TestCase):
    def test_minimal_layers_and_input_isolation(self):
        with tempfile.TemporaryDirectory() as root:
            for layer in ({"CGO_ENABLED": "0"}, {"CGO_ENABLED": "0", "GOMAXPROCS": "2"}):
                before = dict(layer)
                actual = parser_env(layer, Path(root) / "cache with spaces")
                self.assertEqual(layer, before)
                self.assertEqual(set(actual) - set(layer), {"GOCACHE", "GOTMPDIR"})
                for key, value in layer.items():
                    self.assertEqual(actual[key], value)
                self.assertTrue(Path(actual["GOCACHE"]).is_dir())
                self.assertTrue(Path(actual["GOTMPDIR"]).is_dir())

    def test_replaces_only_parser_cache_settings(self):
        with tempfile.TemporaryDirectory() as root:
            original = {"GOCACHE": "off", "GOTMPDIR": "absent", "TEMP": "kept", "PATH": "kept"}
            result = parser_env(original, Path(root) / "cache")
            self.assertEqual(original["GOCACHE"], "off")
            self.assertEqual(original["GOTMPDIR"], "absent")
            self.assertEqual(result["TEMP"], "kept")
            self.assertEqual(result["PATH"], "kept")

    def test_rejects_relative_path(self):
        with self.assertRaises(ValueError):
            parser_env({}, "relative-cache")

    def test_cache_and_temp_path_errors_propagate(self):
        with tempfile.TemporaryDirectory() as root:
            base = Path(root)
            obstacle = base / "cache"
            obstacle.write_bytes(b"not a directory")
            with self.assertRaises(OSError):
                parser_env({}, obstacle)
            obstacle.unlink()
            (base / "go-tool-tmp").write_bytes(b"not a directory")
            with self.assertRaises(OSError):
                parser_env({}, obstacle)


if __name__ == "__main__":
    unittest.main()
