#!/usr/bin/env python3
"""Launch a native GoIM ZIP and verify the AppKit loop on the process main thread."""

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time


THREAD_HEADER = re.compile(r'^\s+\d+\s+Thread_\S+')
APPKIT_LOOP = re.compile(r'-\[NSApplication(?:\([^)]*\))? (?:run\]|_?nextEventMatchingEventMask:)')


def appkit_main_loop(report):
    on_main = False
    for line in report.splitlines():
        if THREAD_HEADER.match(line):
            on_main = 'com.apple.main-thread' in line
        elif not line.strip():
            on_main = False
        elif on_main and APPKIT_LOOP.search(line):
            return line.strip()
    return None


def sample_launch(process, report_path, sample_log):
    deadline = time.monotonic() + 40
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f'GoIM exited during launch with status {process.returncode}')

        report_path.unlink(missing_ok=True)
        try:
            result = subprocess.run(
                ['/usr/bin/sample', str(process.pid), '1', '10', '-file', str(report_path)],
                stdout=sample_log,
                stderr=subprocess.STDOUT,
                timeout=6,
            )
        except subprocess.TimeoutExpired:
            continue

        if result.returncode == 0 and report_path.exists():
            report = report_path.read_text(encoding='utf-8', errors='replace')
            if frame := appkit_main_loop(report):
                if process.poll() is None:
                    return frame

        time.sleep(1)

    raise RuntimeError('AppKit event loop was not observed on com.apple.main-thread')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('zip', type=Path)
    parser.add_argument('arch', choices=('arm64', 'amd64'))
    parser.add_argument('diagnostics', type=Path)
    args = parser.parse_args()
    if sys.platform != 'darwin':
        parser.error('this launch verification requires a native macOS runner')

    diagnostics = args.diagnostics.resolve()
    diagnostics.mkdir(parents=True, exist_ok=True)
    launch_log = diagnostics / 'goim.log'
    sample_log = diagnostics / 'sample.log'
    report_path = diagnostics / 'appkit-main-thread.sample.txt'
    runner_arch = os.uname().machine
    expected_arch = {'arm64': 'arm64', 'amd64': 'x86_64'}[args.arch]
    if runner_arch != expected_arch:
        parser.error(f'expected a native {expected_arch} runner, found {runner_arch}')

    try:
        with tempfile.TemporaryDirectory(prefix='goim-macos-startup-') as temp:
            root = Path(temp)
            subprocess.run(
                ['/usr/bin/ditto', '-x', '-k', str(args.zip.resolve()), str(root)],
                check=True,
                timeout=30,
            )
            app = root / 'GoIM.app'
            executable = app / 'Contents' / 'MacOS' / 'GoIM'
            subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(app)],
                           check=True, timeout=20)
            subprocess.run([str(executable), '--version'], check=True, timeout=10)

            run_env = os.environ.copy()
            run_env.pop('VIVA_HEADLESS', None)
            with launch_log.open('wb') as out, sample_log.open('wb') as sampling:
                process = subprocess.Popen(
                    [str(executable), '--data-dir', str(root / 'goim-data'),
                     '--download-dir', str(root / 'downloads'), '--listen', '127.0.0.1:0',
                     '--no-discovery', '--no-ipmsg', '--no-compatibility'],
                    env=run_env,
                    stdin=subprocess.DEVNULL,
                    stdout=out,
                    stderr=subprocess.STDOUT,
                )
                try:
                    frame = sample_launch(process, report_path, sampling)
                finally:
                    if process.poll() is None:
                        process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
        print(f'Native {args.arch} GoIM reached AppKit on com.apple.main-thread: {frame}')
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(error, file=sys.stderr)
        for log in (launch_log, sample_log):
            if log.exists():
                print(log.read_text(encoding='utf-8', errors='replace')[-5000:], file=sys.stderr)
        return 1

    return 0


if __name__ == '__main__':
    sys.exit(main())
