"""VT protocol regression checks; no child process, service or VM is started."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("pty_smoke", Path(__file__).with_name("pty-smoke.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
Screen = module.Screen


class ScreenTests(unittest.TestCase):
    def test_lightning_text_selector_shares_its_wide_cell(self):
        screen = Screen(6, 1)
        screen.feed("⚡︎X".encode())
        self.assertEqual(screen.cells[0][:3], ["⚡︎", "", "X"])
        self.assertEqual(module.cell_width("⚡︎X"), 3)

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

    def test_insert_mode_preserves_suffix_when_uptime_resets(self):
        screen = Screen(12, 1)
        screen.feed(b"Up for 1m\x1b[8G<\x1b[4")
        screen.feed(b"h1\x1b[4l")
        self.assertEqual(screen.text(), "Up for <1m")
        screen.feed(b"X")
        self.assertEqual(screen.text(), "Up for <1X")

    def test_setting_control_finds_wrapped_action_without_selecting_an_adjacent_setting(self):
        screen = Screen(48, 4)
        screen.feed("  终端代理    ○ 关闭      配置\r\n\r\n  虚拟网卡\r\n     ● 开启               配置".encode())
        self.assertEqual(screen.setting_control("终端代理", "配置"), (26, 0))
        self.assertEqual(screen.setting_control("虚拟网卡", "配置"), (26, 3))
        self.assertIsNone(screen.setting_control("终端代理", "● 开启"))

    def test_insert_mode_moves_styles_with_cells_for_wide_text(self):
        spec = importlib.util.spec_from_file_location("onboarding_pty", Path(__file__).with_name("onboarding-pty.py"))
        preview = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(preview)
        screen = preview.StyledScreen(8, 1)
        screen.feed(b"\x1b[31mABC")
        first = screen.style
        screen.feed(b"\x1b[34mDEFGH")
        second = screen.style
        screen.feed("\x1b[3G\x1b[32m\x1b[4h新\x1b[4l".encode())
        self.assertEqual(screen.cells[0], ["A", "B", "新", "", "C", "D", "E", "F"])
        self.assertEqual(screen.styles[0], [first] * 2 + [screen.style] * 2 + [first] + [second] * 3)


if __name__ == "__main__":
    unittest.main()
