"""VT protocol regression checks; no child process, service or VM is started."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("pty_smoke", Path(__file__).with_name("pty-smoke.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
Screen = module.Screen


class ScreenTests(unittest.TestCase):
    def populated(self):
        screen = Screen(8, 6)
        screen.feed(b"\x1b[Hheader")
        screen.feed(b"\x1b[2;1HAAAAA\x1b[3;1HBBBBB\x1b[4;1HCCCCC\x1b[5;1HDDDDD\x1b[6;1Hfooter")
        return screen

    def test_hardware_scroll_preserves_fixed_header_and_footer(self):
        screen = self.populated()
        # The renderer removes the onboarding block using terminal scrolling,
        # then writes only changed cells. Header/footer must stay in place.
        screen.feed(b"\x1b[2;5r\x1b[2S\x1b[4;1HNEW\x1b[r")
        self.assertEqual(screen.text().splitlines(), ["header", "CCCCC", "DDDDD", "NEW", "", "footer"])
        self.assertEqual((screen.top, screen.bottom), (0, 5))

    def test_insert_delete_lines_and_reverse_index_respect_margins(self):
        screen = self.populated()
        screen.feed(b"\x1b[2;5r\x1b[3;1H\x1b[M\x1b[LXX")
        self.assertEqual(screen.text().splitlines(), ["header", "AAAAA", "XX", "CCCCC", "DDDDD", "footer"])
        screen.feed(b"\x1b[2;1H\x1bM")
        self.assertEqual(screen.text().splitlines(), ["header", "", "AAAAA", "XX", "CCCCC", "footer"])

    def test_newline_and_scroll_down_with_split_sequences(self):
        screen = self.populated()
        for chunk in (b"\x1b[2;", b"5r\x1b[5;1H\n", b"\x1b[", b"T"):
            screen.feed(chunk)
        self.assertEqual(screen.text().splitlines(), ["header", "", "BBBBB", "CCCCC", "DDDDD", "footer"])

    def test_backward_tab_used_by_cursor_optimization(self):
        screen = Screen(30, 2)
        screen.feed(b"\x1b[25G\x1b[Zx\x1b[2Zy")
        self.assertEqual(screen.cells[0][16], "x")
        self.assertEqual(screen.cells[0][8], "y")


if __name__ == "__main__":
    unittest.main()
