# Ports between the host and the VM

The VM has no address that the host can connect to. The VM cannot connect to the host. To connect
them, declare ports by name with [`expose`](reference/options.md#expose) and
[`reach`](reference/options.md#reach). The host opens an SSH tunnel for each port.

```nix
services.sandbox-vm = {
  # Services in the VM, exposed on 127.0.0.1 of the host
  expose = {
    web = 3000;                                 # same port on both sides
    db  = { guest = 5432; host = 15432; };
  };

  # External services that the VM can reach on its 127.0.0.1.
  reach = {
    internal = { host = "my-internal-service"; port = 8080; };
    cache    = { host = "10.0.0.5"; port = 6379; guest = 16379; };
  };
};
```

With this example:

- `http://127.0.0.1:3000` on the host goes to port 3000 in the VM.
- `127.0.0.1:8080` in the VM goes to `my-internal-service:8080` through the host.

WARNING: The VM controls the pages of an exposed web server. The browser on the host gives these
pages the cookies of `127.0.0.1` and `localhost`, also the cookies of other local servers, because
cookies are not different for each port. The pages can also send requests to other services on the
host. Open the pages of the VM in a separate browser profile.

[`reach`](reference/options.md#reach) is the exception to the network rules of the VM:

- The host makes the connection. Thus the host resolves the name.
- The service can have a private address, because the connection does not go through the proxy. It
  can also be the host itself (`host = "localhost"`).
- Programs in the VM must connect without the proxy. The proxy settings already exclude `127.0.0.1`
  and `localhost`.

Other rules:

- If a port is in use on your host, change it in your own `devenv.local.nix`. Example:
  `services.sandbox-vm.expose.db.host = 25432;`.
- If a port is in use, the process shows a message and does not open that port. The VM and the other
  ports continue to work.
- If the process cannot open the ports at all, it shows a message and the VM continues to run
  without them. To try again, run
  [`devenv tasks run sandbox-vm:ports`](reference/commands.md#task-ports).
- Changes apply while the VM runs. A restart is not necessary. Enter the devenv shell again, or run
  [`devenv tasks run sandbox-vm:ports`](reference/commands.md#task-ports).

For a temporary tunnel, you can use SSH directly: `ssh -L 3000:localhost:3000 devenv-<project>`.

## Hostnames for ports

_This is an alternative to [`services.sandbox-vm.expose`](reference/options.md#expose)._

With the native process manager of devenv, a port of the VM can have a hostname on the host, for
example `http://web.localhost`. Declare a process that opens an SSH tunnel to the port, and give the
process a hostname. For the proxy, refer to
[the devenv documentation](https://devenv.sh/processes/).

```nix
# Only the native process manager has the proxy.
process.proxy.enable = true;

processes.web = {
  proxy.hostname = "web.localhost";
  ports.http.allocate = 8080;
  env.PORT = toString config.processes.web.ports.http.value;
  # Port 3000 from inside the VM is exposed on http://web.localhost on the host:
  exec = "ssh -N -L $PORT:localhost:3000 ${config.services.sandbox-vm.name}";
  after = [ "devenv:processes:sandbox-vm" ];
};
```

`http://web.localhost` on the host then goes to port 3000 in the VM. `ports.http.allocate` finds a
free port on the host, so a port in use is not a problem. Start the tunnel together with the VM:
[`devenv up sandbox-vm web`](reference/commands.md#sandbox-vm).
