# Makes the reference pages from the JSON of docs/reference.nix (on stdin), in the directory
# of the first argument: options.md from host/options.nix, and commands.md from
# host/commands.nix. Do not edit the results: change the Nix files, then run `docs-reference`.
import json
import pathlib
import re
import sys

from links import PREFIX, Names, anchor

GROUPS = [
    ("General", ""),
    ("Proxy", "proxy."),
    ("Ports", "expose"),
    ("Ports", "reach"),
    ("Backup", "backup."),
]
HEADER = "<!-- Made by docs/reference.py from host/{}. Do not edit. -->"


def group(name):
    for title, start in GROUPS[1:]:
        if name.startswith(start):
            return title
    return "General"


def describe(text, own, page):
    """The text with links to the options, processes and tasks that it names in backticks.
    `own` is the item that the text describes, and `page` the page that it is on. A name
    without backticks is an error: it would have no link."""
    bare = re.sub(r"`[^`]*`", "", text)
    for word in re.findall(r"[\w.:<>-]+", bare):
        word = word.strip(".:")
        option = re.search(r"[A-Z.]", word) and names.option(word, own)
        if (option and option != own) or word in names.tasks:
            sys.exit(f"{own}: write `{word}` in backticks, so that it gets a link")

    def link(m):
        span = m.group(1)
        target, on = names.option(span, own), "options.md"
        if target is None:
            target, on = names.command(span), "commands.md"
        if target is None or target == own:
            return m.group(0)
        return f"[{m.group(0)}]({'' if on == page else on}#{anchor(target)})"

    return re.sub(r"`([^`]+)`", link, text.strip())


def value(v):
    text = v["text"]
    if "\n" in text:
        return "\n\n  ```nix\n" + "\n".join("  " + line for line in text.splitlines()) + "\n  ```"
    return f" `{text}`"


data = json.load(sys.stdin)
options = [
    o
    for o in data["options"]
    if o["visible"] and not o["internal"] and "._module." not in o["name"]
]
commands = data["commands"]
names = Names(
    [o["name"].removeprefix(PREFIX) for o in options],
    commands["processes"],
    commands["tasks"],
)
out_dir = pathlib.Path(sys.argv[1])

out = [
    HEADER.format("options.nix"),
    "",
    "# Options",
    "",
    "Set these options in the `devenv.nix` of your project. All options are optional, except",
    "`services.sandbox-vm.enable`.",
]
for title in dict.fromkeys(t for t, _ in GROUPS):
    out += ["", f"## {title}"]
    for o in options:
        short = o["name"].removeprefix(PREFIX)
        if group(short) != title:
            continue
        out += ["", f"### `{o['name']}` {{#{anchor(short)}}}", ""]
        out += [describe(o["description"], short, "options.md"), ""]
        # A type can have a pattern, for example [a-z]([a-z])?, that Markdown reads as a link.
        out.append(f"- Type: `{o['type']}`")
        if "default" in o:
            out.append("- Default:" + value(o["default"]))
        if "example" in o:
            out.append("- Example:" + value(o["example"]))
(out_dir / "options.md").write_text("\n".join(out) + "\n")

out = [
    HEADER.format("commands.nix"),
    "",
    "# Processes and tasks",
    "",
    "Run these commands on the host, in the directory of your project.",
]
# A process has no command here: its text says how to start it, because some processes start
# only together with others.
for title, kind in [("Processes", "processes"), ("Tasks", "tasks")]:
    out += ["", f"## {title}"]
    for name, text in sorted(commands[kind].items()):
        out += ["", f"### `{name}` {{#{anchor(name)}}}", ""]
        if kind == "tasks":
            out += ["```bash", f"devenv tasks run {name}", "```", ""]
        out += [describe(text, name, "commands.md")]
(out_dir / "commands.md").write_text("\n".join(out) + "\n")
