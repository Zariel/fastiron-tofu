import os
import pty
import select
import threading
import unittest
from unittest.mock import patch

from serial_console import Console, ConsoleError, redact


class ConsoleTest(unittest.TestCase):
    def test_session(self):
        master, slave = pty.openpty()
        self.addCleanup(os.close, master)
        self.addCleanup(os.close, slave)
        stop = threading.Event()
        commands = []
        responses = [
            ("", "Username:"),
            ("operator", "Password:"),
            ("login-secret", "\r\nswitch>"),
            ("enable", "Password:"),
            ("enable-secret", "\r\nswitch#"),
            ("skip-page-display", "skip-page-display\r\nswitch#"),
            ("show version", "show version\r\nSW: Version 09.0.10kT213\r\nswitch#"),
            ("bad command", "bad command\r\n% Invalid input: private-value\r\nswitch#"),
        ]

        def firmware():
            pending = b""
            while not stop.is_set() and len(commands) < len(responses):
                if not select.select([master], [], [], 0.05)[0]:
                    continue
                pending += os.read(master, 4096)
                while b"\r" in pending:
                    line, pending = pending.split(b"\r", 1)
                    commands.append(line.decode())
                    reply = responses[len(commands) - 1][1].encode()
                    # Deliver a split prompt to exercise serial stream framing.
                    os.write(master, reply[:-1])
                    os.write(master, reply[-1:])

        with patch.dict(os.environ, {
            "FASTIRON_USERNAME": "operator",
            "FASTIRON_PASSWORD": "login-secret",
            "FASTIRON_ENABLE_PASSWORD": "enable-secret",
        }):
            with Console(os.ttyname(slave), 9600, 2, 2) as console:
                worker = threading.Thread(target=firmware)
                worker.start()
                try:
                    console.login()
                    self.assertEqual(console.command("show version"), "SW: Version 09.0.10kT213")
                    with self.assertRaisesRegex(ConsoleError, "batch stopped"):
                        console.command("bad command")
                finally:
                    stop.set()
                    worker.join(timeout=2)
                    self.assertFalse(worker.is_alive())
        self.assertEqual(commands, [command for command, _ in responses])

    def test_redaction(self):
        with patch.dict(os.environ, {"FASTIRON_PASSWORD": "credential-marker"}):
            self.assertEqual(
                redact("username super password 8 opaque-hash\nhello credential-marker\nvlan 53 by port"),
                "[secret configuration redacted]\nhello [redacted]\nvlan 53 by port",
            )

    def test_control_characters(self):
        console = Console("unused", 9600, 1, 1)
        with self.assertRaisesRegex(ConsoleError, "control characters"):
            console.send("show version\rwrite memory")


if __name__ == "__main__":
    unittest.main()
