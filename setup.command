#!/bin/bash
# Sets up everything the test scripts in tests/ need.
# The site itself needs a C++ compiler, Python 3 and Go (see README); this only installs Playwright for tests/shoot.py.
set -e
cd "$(dirname "$0")"

echo "== ascii-site setup =="

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 was not found. macOS will now offer to install the Command Line Tools,"
  echo "which include python3. Accept that, then double-click setup.command again."
  xcode-select --install || true
  read -n 1 -s -r -p "Press any key to close."
  exit 1
fi

echo "Using $(python3 --version) at $(command -v python3)"

if [ ! -d .venv ]; then
  echo "Creating a virtual environment in .venv ..."
  python3 -m venv .venv
fi

echo "Installing Playwright ..."
./.venv/bin/python -m pip install --upgrade pip
./.venv/bin/python -m pip install playwright

echo "Downloading Playwright's Chromium ..."
./.venv/bin/python -m playwright install chromium

echo ""
echo "Done. To run the tests from this folder:"
echo "  .venv/bin/python tests/shoot.py        # screenshots go to tests/frames/"
echo "  .venv/bin/python tests/test_builder.py  # builder unit tests"
echo "  make test               # everything"
echo ""
read -n 1 -s -r -p "Press any key to close."
