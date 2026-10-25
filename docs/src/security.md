# Security

## What an agent in the VM can and cannot do

An agent is most useful when it can run your projects and change the system as it likes. On the
host, it can then also reach all your files and credentials. In the VM, it can reach only the files
and access that you give it.

In the VM, the agent **can** do these things:

- **Run your full projects.** The VM is a complete NixOS system, so the agent can install
  dependencies, start services and run the tests of the repos that you put there.
- **Change the system.** You and the agent have root access in the VM. If the agent causes damage,
  delete the VM. Then [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) creates a new VM
  from the committed files.

The agent **cannot** do these things:

- **Read your files.** The host shares no directory with the VM. The VM has only the files that you
  put there.
- **Use your SSH keys.** The host does not forward your ssh-agent. Thus the agent cannot log in to
  your servers.

  WARNING: Do not forward your ssh-agent to the VM (`ssh -A`, or `ForwardAgent yes` in your SSH
  configuration). If you do, the agent can use your keys. The `ssh.config` of the VM sets
  `ForwardAgent no`, `ForwardX11 no`, `ForwardX11Trusted no` and `PermitLocalCommand no`. These
  settings have an effect only if the `Include` line is before other settings in `~/.ssh/config`.
  They do not stop `ssh -A` or `ssh -X`. `ssh.config` cannot remove the `LocalForward`,
  `RemoteForward` and `SendEnv` settings of a `Host *` block. Do not use these settings for all
  hosts: with `SendEnv`, the VM gets the values of your environment variables.

- **Push to your repos.** The VM has no credentials for your remotes, unless you give them. The VM
  cannot get access to the repo on the host. You decide how work moves in and out. Refer to
  [How to put your repos in the VM](repos.md).
- **Open ports on the host.** The host opens only the SSH forward of the VM, the port of the proxy,
  and the ports that you declare with [`services.sandbox-vm.expose`](reference/options.md#expose).
  All these ports are on `127.0.0.1`.
- **Connect to the host.** The VM cannot connect to a service on the host. This is true for
  `127.0.0.1` and for the other addresses of the host. The exceptions are the proxy and the services
  that you declare with [`services.sandbox-vm.reach`](reference/options.md#reach).
- **Connect to your local network.** The VM cannot connect to computers with private addresses, for
  example your router or a NAS. The exceptions are the names and ranges that you open with
  [`proxy.allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains) and
  [`proxy.allowRanges`](reference/options.md#proxy-allowranges). Refer to [How to limit the
  destinations](network.md#how-to-limit-the-destinations). The proxy uses only IPv4 by default,
  because on a network with public IPv6 addresses it cannot know which addresses are local.
- **Bypass the proxy.** Each connection from the VM goes through an HTTP proxy on the host. The
  proxy also resolves the host names. The VM has no DNS and no other route out. The only exceptions
  are the services that you declare with [`services.sandbox-vm.reach`](reference/options.md#reach).
  Refer to [The network of the VM](network.md).

By default, the proxy permits connections to all hosts on the internet. Thus an agent can send the
data of the VM to a host of its choice. To prevent this, give the list of the hosts that the VM can
connect to. Refer to [How to limit the destinations](network.md#how-to-limit-the-destinations).

WARNING: Code from the VM runs on the host without a sandbox when you run it there. Before you merge
changes from the VM, review all of them, not only the [`proxy`](reference/options.md#proxy) and
[`reach`](reference/options.md#reach) options and `sandbox-vm/`. `devenv shell` on the host runs
`devenv.nix` (for example `enterShell` and the tasks) and the imports of `devenv.yaml`. Other
examples are `.envrc`, git hooks, build files, the scripts of a package and the tests. `git fetch`
from the VM does not run code from the VM.

## Limits of the security

devenv-sandbox-vm is experimental. Its design can have problems that the author has not found yet.

The project does not set one level of security. You select the restrictions that fit your risks, and
each restriction makes the VM less practical to use:

- **A strict list of allowed hosts can be impractical.** With
  [`proxy.allowedDomains`](reference/options.md#proxy-alloweddomains), each tool and package manager
  in the VM needs its hosts in the list. If a host is missing, installs and updates fail.
- **Some hosts that the VM needs are also a way to send data out.** NixOS needs GitHub to install
  and update software, because Nixpkgs and most flake inputs are there. An attacker in the VM can
  also use GitHub to send data out, for example with a push to a repo of the attacker. Refer to
  [How to limit the destinations](network.md#how-to-limit-the-destinations).
- **The sandbox does not make the software in the VM safe.** A package manager such as npm runs code
  from many authors when it installs packages. The sandbox limits what that code can reach, but the
  code can read and send all data in the VM.

In the author's opinion, the largest gains do not depend on these trade-offs: the VM cannot read the
files of your computer, it cannot connect to other computers on your LAN, and it does not get your
SSH keys. Refer to
[What an agent in the VM can and cannot do](#what-an-agent-in-the-vm-can-and-cannot-do).

For very sensitive data, a sandbox on your own computer is possibly not sufficient. Do that work on
a separate computer, and do not install packages from public registries such as npm there.

No sandbox is perfect. Decide which risks you accept in exchange for a practical environment.
