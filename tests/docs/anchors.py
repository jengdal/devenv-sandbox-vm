# Checks the links of the documentation. Run it after `mdbook build docs`.
#
# - Each link to a heading (page.md#heading or #heading) finds the heading in the HTML that
#   mdBook built. mdbook-linkcheck2 checks only that the pages exist: it does not implement
#   fragment checks.
# - Each option, process or task name in backticks links to its reference page, outside code
#   blocks and headings. The reference pages have the same rule (docs/reference.py), and
#   docs/links.py has the rules for the names.
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(root / "docs"))
from links import PREFIX, Names

# Relative to the current directory, for short messages.
docs = (root / "docs").relative_to(pathlib.Path.cwd(), walk_up=True)
src = docs / "src"
html = docs / "book/html"
reference = src / "reference"


def headings(page):
    """The names in the ### headings of a reference page."""
    return re.findall(r"^### `(\S+)` \{#", (reference / page).read_text(), re.MULTILINE)


commands = headings("commands.md")
names = Names(
    [n.removeprefix(PREFIX) for n in headings("options.md")],
    [n for n in commands if ":" not in n],
    [n for n in commands if ":" in n],
)


def paragraphs(lines):
    """The paragraphs and list items outside code blocks and headings, as (line number,
    text) with the lines joined: a code span can continue on the next line."""
    fence, start, text = False, 0, []
    for n, line in enumerate(lines, 1):
        # A fence in a list item is indented. A line that starts with # is a heading only
        # without indentation.
        is_fence = line.lstrip().startswith("```")
        if is_fence:
            fence = not fence
        skip = fence or is_fence or not line.strip() or line.startswith("#")
        if skip or re.match(r"\s*(- |\d+\. )", line):
            if text:
                yield start, " ".join(text)
            text = []
            if skip:
                continue
        if not text:
            start = n
        text.append(line.strip())
    if text:
        yield start, " ".join(text)


failures = []
for page in sorted(src.rglob("*.md")):
    lines = page.read_text().splitlines()
    for n, line in enumerate(lines, 1):
        for target, heading in re.findall(r"\]\(([\w./-]*?)#([\w-]+)\)", line):
            md = (page.parent / target).resolve() if target else page.resolve()
            built = html / md.relative_to(src.resolve()).with_suffix(".html")
            if f'id="{heading}"' not in built.read_text():
                failures.append(f"{page}:{n}: no heading #{heading} in {built}")
    if page.parent == reference:
        continue
    for n, text in paragraphs(lines):
        # A name in backticks that is not in the text of a link. The backticks pair in order.
        links = [m.span(1) for m in re.finditer(r"\[((?:`[^`]*`|[^\]`])*)\]\(", text)]
        for m in re.finditer(r"`([^`]+)`", text):
            span = m.group(1)
            if any(start <= m.start() and m.end() <= end for start, end in links):
                continue
            if names.option(span):
                failures.append(f"{page}:{n}: link `{span}` to the Options page")
            elif names.command(span):
                failures.append(f"{page}:{n}: link `{span}` to the Processes and tasks page")
print("\n".join(failures))
sys.exit(1 if failures else 0)
