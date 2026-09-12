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
            ("show hardware", "show hardware\r\nSerial #"),
            ("show version", "show version\r\nSW: Version 09.0.10kT213\r\nswitch#"),
            ("configure terminal", "configure terminal\r\nswitch(config)#"),
            ("ipv6 access-list TOFU-REBOOT-V6", "ipv6 access-list TOFU-REBOOT-V6\r\nswitch(config-ipv6-access-list TOFU-REBOOT-V6)#"),
            ("end", "end\r\nswitch#"),
            ("configure terminal", "configure terminal\r\nswitch(config)#"),
            ("ipv6 router pim", "ipv6 router pim\r\nipv6 unicast-routing must be enabled before ipv6 PIM can be enabled\r\nswitch(config)#"),
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
                    if line == b"show hardware":
                        # A read boundary after '#' in ordinary output must not
                        # terminate the response or shift the following reply.
                        stop.wait(0.02)
                        os.write(master, b":ABC123\r\nswitch#")

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
                    self.assertEqual(console.command("show hardware"), "Serial #:ABC123")
                    self.assertEqual(console.command("show version"), "SW: Version 09.0.10kT213")
                    self.assertEqual(console.command("configure terminal"), "")
                    self.assertEqual(console.command("ipv6 access-list TOFU-REBOOT-V6"), "")
                    self.assertEqual(console.command("end"), "")
                    self.assertEqual(console.command("configure terminal"), "")
                    with self.assertRaisesRegex(ConsoleError, "must be enabled before"):
                        console.command("ipv6 router pim")
                    with self.assertRaisesRegex(ConsoleError, "batch stopped"):
                        console.command("bad command")
                finally:
                    stop.set()
                    worker.join(timeout=2)
                    self.assertFalse(worker.is_alive())
        self.assertEqual(commands, [command for command, _ in responses])

    def test_boot_prompt(self):
        for prompt in ("ICX7150-Boot>", "uboot>", "Boot>"):
            with self.subTest(prompt=prompt):
                master, slave = pty.openpty()
                try:
                    with Console(os.ttyname(slave), 9600, 1, 1) as console:
                        os.write(master, prompt.encode())
                        with self.assertRaisesRegex(ConsoleError, "boot-monitor"):
                            console.read(1)
                finally:
                    os.close(master)
                    os.close(slave)

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

    def test_aaa_keys(self):
        for command in (
            "radius-server host 192.0.2.53 auth-port 1812 key test-radius-value",
            "tacacs-server host 192.0.2.54 auth-port 49 key 2 encoded-value",
            "radius-server key global-radius-value",
            "tacacs-server key global-tacacs-value",
        ):
            with self.subTest(command=command.split(" key")[0]):
                self.assertEqual(redact(command), "[secret configuration redacted]")


if __name__ == "__main__":
    unittest.main()
