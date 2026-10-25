# The names that the documentation links to its reference pages: options, processes and
# tasks. docs/reference.py adds the links, and tests/docs/anchors.py checks the other pages.
import re

PREFIX = "services.sandbox-vm."
GROUPS = ("proxy", "backup")


def anchor(name):
    """The anchor of an option, a process or a task: proxy.allowedDomains is
    proxy-alloweddomains, sandbox-vm:update is task-update. A task has its own form, because
    sandbox-vm:backup and the process sandbox-vm-backup would otherwise be the same."""
    if name.startswith("sandbox-vm:"):
        return "task-" + name.removeprefix("sandbox-vm:")
    return name.replace("<name>", "name").replace(".", "-").lower()


class Names:
    def __init__(self, options, processes, tasks):
        self.options = set(options)  # without PREFIX
        self.processes = set(processes)
        self.tasks = set(tasks)
        # allowedDomains for proxy.allowedDomains: the last part of exactly one option.
        last = {}
        for name in self.options:
            last.setdefault(name.split(".")[-1], []).append(name)
        self.last = {part: names[0] for part, names in last.items() if len(names) == 1}

    def option(self, name, own=""):
        """The option that a name in backticks names, or None. In the description of the
        option `own`, a name can also be in the group of `own` (allowedDomains in proxy.ipv6).
        A short name counts only if it cannot be a normal word: with a dot or a capital
        letter, or expose and reach. A group (proxy, backup) is its own name."""
        if name.startswith(PREFIX):
            name = name.removeprefix(PREFIX)
            return name if name in self.options or name in GROUPS else None
        if name in GROUPS:
            return name
        if not (re.search(r"[A-Z.]", name) or name in ("expose", "reach")):
            return None
        parts = own.split(".")[:-1]
        for n in range(len(parts), -1, -1):
            candidate = ".".join(parts[:n] + [name])
            if candidate in self.options:
                return candidate
        return self.last.get(name)

    def command(self, span):
        """The task or process that a code span names, or None: a task anywhere in the span
        (devenv tasks run sandbox-vm:update), the last process of `devenv up …`, or the span
        that is only the name of a process."""
        for task in re.findall(r"sandbox-vm:[\w-]+", span):
            if task in self.tasks:
                return task
        if span.startswith("devenv up "):
            found = [word for word in span.split()[2:] if word in self.processes]
            return found[-1] if found else None
        return span if span in self.processes else None
