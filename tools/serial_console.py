#!/usr/bin/env python3
"""Run framed FastIron commands over a serial console using only the standard library."""

import argparse
import contextlib
import fcntl
import json
import os
import re
import select
import sys
import termios
import time


ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
PROMPT = re.compile(r"(?m)^([A-Za-z0-9_.:/ @-]+(?:\([^\r\n()]*\))?[>#])\s*$")
LOGIN = re.compile(r"(?i)(?:user ?name|login):\s*$")
PASSWORD = re.compile(r"(?i)password:\s*$")
ERROR = re.compile(
    r"(?im)^\s*(?:%\s*(?:error|invalid|unknown|incomplete|ambiguous)|"
    r"error\s*[:\-]|invalid (?:input|command)|unknown command|"
    r"incomplete command|ambiguous command|not authorized|permission denied|login incorrect)"
)


class ConsoleError(Exception):
    pass


def clean(text):
    text = ANSI.sub("", text).replace("\r\n", "\n").replace("\r", "\n")
    result = []
    for char in text:
        if char == "\b":
            if result:
                result.pop()
        else:
            result.append(char)
    return "".join(result)


def redact(output):
    # Configuration secret fields are redacted as whole lines, including hashes.
    secret = re.compile(
        r"(?i)\b(?:password|secret|community|authentication-key|"
        r"auth-key|key-string|private-key|shared-secret|key)\b"
    )
    lines = ["[secret configuration redacted]" if secret.search(line) else line for line in output.splitlines()]
    for name in ("FASTIRON_PASSWORD", "FASTIRON_ENABLE_PASSWORD"):
        value = os.environ.get(name)
        if value:
            lines = [line.replace(value, "[redacted]") for line in lines]
    return "\n".join(lines)


class Console:
    def __init__(self, device, baud, wait, timeout, debug=False):
        self.device = device
        self.baud = baud
        self.wait = wait
        self.timeout = timeout
        self.debug = debug
        self.fd = None
        self.settings = None
        self.prompt_name = None

    def __enter__(self):
        deadline = time.monotonic() + self.wait
        while True:
            try:
                self.fd = os.open(self.device, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
                break
            except FileNotFoundError:
                if time.monotonic() >= deadline:
                    raise ConsoleError("serial device did not appear before the deadline") from None
                time.sleep(0.25)
        try:
            # The lock covers login and the complete batch, preventing two copies
            # of this tool from interleaving commands on the same console.
            fcntl.flock(self.fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.settings = termios.tcgetattr(self.fd)
            attrs = termios.tcgetattr(self.fd)
            speed = getattr(termios, f"B{self.baud}", None)
            if speed is None:
                raise ConsoleError("unsupported console baud rate")
            attrs[0] = termios.IGNBRK
            attrs[1] = 0
            attrs[2] = termios.CS8 | termios.CREAD | termios.CLOCAL
            attrs[3] = 0
            attrs[4] = attrs[5] = speed
            attrs[6][termios.VMIN] = 0
            attrs[6][termios.VTIME] = 0
            termios.tcsetattr(self.fd, termios.TCSANOW, attrs)
        except Exception:
            self.__exit__(None, None, None)
            raise
        return self

    def __exit__(self, *_):
        if self.fd is not None:
            if self.settings is not None:
                with contextlib.suppress(OSError, termios.error):
                    termios.tcsetattr(self.fd, termios.TCSANOW, self.settings)
            os.close(self.fd)
            self.fd = None

    def send(self, line):
        if any(ord(char) < 32 or ord(char) == 127 for char in line):
            raise ConsoleError("commands and credentials must not contain control characters")
        data = (line + "\r").encode()
        deadline = time.monotonic() + self.timeout
        while data:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([], [self.fd], [], remaining)[1]:
                raise ConsoleError("serial write timed out")
            data = data[os.write(self.fd, data):]

    def read(self, timeout):
        deadline = time.monotonic() + timeout
        data = bytearray()
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            if not select.select([self.fd], [], [], remaining)[0]:
                break
            chunk = os.read(self.fd, 65536)
            if not chunk:
                raise ConsoleError("serial connection closed")
            data.extend(chunk)
            if len(data) > 8 << 20:
                raise ConsoleError("serial output exceeds size limit")
            output = clean(data.decode(errors="replace"))
            if LOGIN.search(output):
                return "login", output
            if PASSWORD.search(output):
                return "password", output
            prompts = list(PROMPT.finditer(output))
            if prompts and prompts[-1].end() == len(output):
                prompt = prompts[-1].group(1).strip()
                name = re.sub(r"\([^()]*\)$", "", prompt[:-1]).strip()
                if self.prompt_name is not None and name != self.prompt_name:
                    continue
                # Boot monitors use a root prompt; configuration names can
                # contain "boot" without changing the switch's operating mode.
                if re.search(r"(?:^|[- ])(?:boot|uboot)>$", prompt, re.IGNORECASE):
                    raise ConsoleError("switch is at a boot-monitor prompt; batch stopped")
                return prompt, output[:prompts[-1].start()]
        if self.debug and data:
            print("Console response: " + repr(redact(clean(data.decode(errors="replace")))[-1000:]), file=sys.stderr)
        raise ConsoleError(f"timed out waiting for a complete switch prompt ({len(data)} bytes received)")

    def login(self):
        # Discard output left by an interrupted command before establishing a
        # fresh prompt. Never associate that output with a new command batch.
        deadline = time.monotonic() + self.wait
        while select.select([self.fd], [], [], 0.1)[0]:
            os.read(self.fd, 65536)
            if time.monotonic() >= deadline:
                raise ConsoleError("console did not become quiet before login")
        self.send("")
        deadline = time.monotonic() + self.wait
        attempts = 0
        while time.monotonic() < deadline:
            prompt, _ = self.read(max(0.1, deadline - time.monotonic()))
            if prompt == "login":
                attempts += 1
                if attempts > 3:
                    raise ConsoleError("console authentication failed")
                username = os.environ.get("FASTIRON_USERNAME")
                if not username:
                    raise ConsoleError("console login requires FASTIRON_USERNAME")
                self.send(username)
            elif prompt == "password":
                password = os.environ.get("FASTIRON_PASSWORD")
                if not password:
                    raise ConsoleError("console login requires FASTIRON_PASSWORD")
                self.send(password)
            elif prompt.endswith(">"):
                self.prompt_name = prompt[:-1].strip()
                self.send("enable")
                prompt, _ = self.read(self.timeout)
                if prompt == "password":
                    password = os.environ.get("FASTIRON_ENABLE_PASSWORD")
                    if not password:
                        raise ConsoleError("privilege elevation requires FASTIRON_ENABLE_PASSWORD")
                    self.send(password)
                    prompt, _ = self.read(self.timeout)
                if not prompt.endswith("#"):
                    raise ConsoleError("console privilege elevation failed")
                self.command("skip-page-display")
                return
            else:
                self.prompt_name = re.sub(r"\([^()]*\)$", "", prompt[:-1]).strip()
                if "(" in prompt:
                    self.command("end")
                self.command("skip-page-display")
                return
        raise ConsoleError("switch did not become ready before the deadline")

    def command(self, command):
        self.send(command)
        prompt, output = self.read(self.timeout)
        if prompt in ("login", "password"):
            raise ConsoleError("console unexpectedly requested authentication")
        output = output.strip()
        if output == command:
            output = ""
        elif output.startswith(command + "\n"):
            output = output[len(command) + 1:]
        if ERROR.search(output):
            raise ConsoleError("switch rejected a command; batch stopped: " + redact(output))
        return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--device", default="/dev/ttyUSB0")
    parser.add_argument("--baud", type=int, default=9600)
    parser.add_argument("--wait", type=float, default=60, help="Seconds to wait for the device and login")
    parser.add_argument("--timeout", type=float, default=30, help="Seconds allowed per command")
    parser.add_argument("--command-file", help="Read one command per line from a file")
    parser.add_argument("--debug", action="store_true", help="Show a redacted response tail on timeout")
    parser.add_argument("commands", nargs="*")
    args = parser.parse_args()
    if args.wait <= 0 or args.timeout <= 0:
        parser.error("timeouts must be positive")
    commands = args.commands
    if args.command_file:
        if commands:
            parser.error("use either command arguments or --command-file")
        with open(args.command_file) as source:
            commands = [line.strip() for line in source if line.strip()]
    if not commands:
        parser.error("at least one command is required")
    try:
        with Console(args.device, args.baud, args.wait, args.timeout, args.debug) as console:
            console.login()
            for command in commands:
                output = console.command(command)
                print(json.dumps({"command": redact(command), "output": redact(output)}), flush=True)
    except (ConsoleError, OSError, termios.error) as error:
        # Exceptions can include device/terminal data; do not dump raw transcripts.
        detail = str(error) if isinstance(error, ConsoleError) else "cannot access or configure serial device"
        if isinstance(error, OSError) and error.errno:
            detail += ": " + os.strerror(error.errno)
        print("Console operation failed: " + detail, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
