# The tests of the host module (host/): the values that Nix creates from the options of
# services.sandbox-vm. They evaluate the module and build nothing. The result is the list
# of the tests that fail; `devenv test` shows it.
#
#   nix-instantiate --eval --strict --json -E 'import ./tests/nix { pkgs = import <nixpkgs> { }; }'
{
  pkgs,
  lib ? pkgs.lib,
}:
let
  constants = import ../../constants.nix;

  inherit (import ./lib.nix { inherit pkgs lib; }) host service proxy;
  # The messages of the assertions that fail.
  failures = options: map (a: a.message) (lib.filter (a: !a.assertion) (host { inherit options; }).assertions);
  # true if the options are not correct: an option refuses its value, or an assertion fails.
  refused =
    options:
    let
      config = host { inherit options; };
      result = builtins.tryEval (
        builtins.deepSeq config.services.sandbox-vm (lib.all (a: a.assertion) config.assertions)
      );
    in
    !result.success || !result.value;

  # The programs of host/scripts.nix for these options, on a host that is supported.
  scripts =
    options:
    let
      cfg = service { inherit options; };
    in
    import ../../host/scripts.nix {
      inherit pkgs lib cfg constants;
      root = "/home/user/project";
      platform = {
        supported = true;
        runtimeInputs = [ ];
        krunkit = pkgs.hello;
        guestArch = "aarch64";
      };
      proxy = proxy options;
    };
  # true if a program sets the variable to the value.
  exports =
    program: name: value:
    lib.hasInfix "\nexport ${name}=${lib.escapeShellArg value}\n" program.text;

  proxySettings = options: removeAttrs (proxy { proxy = options; }).settings [ "acl_file" ];
  acl = options: (proxy { proxy = options; }).aclSettings;

  forwardOptions = {
    expose.web = {
      host = 8080;
      guest = 80;
    };
    expose.same = 3000;
    reach.db = {
      host = "db.example";
      port = 5432;
      guest = 15432;
    };
  };
in
lib.runTests {
  # host/commands.nix documents each process and task of the module, and no other.
  testCommandsDocumented =
    let
      config = host { options.backup.enable = true; };
      commands = import ../../host/commands.nix;
    in
    {
      expr = {
        processes = lib.attrNames config.processes;
        tasks = lib.attrNames config.tasks;
      };
      expected = {
        processes = lib.attrNames commands.processes;
        tasks = lib.attrNames commands.tasks;
      };
    };
  # The default names come from the name of the directory of the project.
  testDefaultName = {
    expr = (service { root = "/home/user/My Project_v2.api"; }).name;
    expected = "devenv-My-Project_v2.api";
  };
  testDefaultHostName = {
    expr = map (root: (service { inherit root; }).hostName) [
      "/home/user/My Project_v2.api"
      "/home/user/-project-"
      "/home/user/${lib.concatStrings (lib.replicate 62 "a")}-bcd"
    ];
    expected = [
      "my-project-v2-api"
      "project"
      (lib.concatStrings (lib.replicate 62 "a"))
    ];
  };

  # One line for each forward, with the fields that host/scripts/ports.sh reads. The test
  # of that script (host/supervisor/scripts_test.go) uses lines of the same form.
  testForwards = {
    expr = exports (scripts forwardOptions).ports "SVM_FORWARDS" (
      "L same 127.0.0.1:3000:127.0.0.1:3000 3000 127.0.0.1:3000 -> port 3000 in the VM\n"
      + "L web 127.0.0.1:8080:127.0.0.1:80 8080 127.0.0.1:8080 -> port 80 in the VM\n"
      + "R db 127.0.0.1:15432:db.example:5432 - port 15432 in the VM -> db.example:5432\n"
    );
    expected = true;
  };
  testNoForwards = {
    expr = exports (scripts { }).ports "SVM_FORWARDS" "";
    expected = true;
  };
  testForwardRules = {
    expr = map refused [
      forwardOptions
      # Two entries with the same port on the host, or with the same port in the VM.
      {
        expose.a = 8080;
        expose.b = {
          host = 8080;
          guest = 80;
        };
      }
      {
        reach.a = {
          host = "a.example";
          port = 1;
          guest = 5000;
        };
        reach.b = {
          host = "b.example";
          port = 2;
          guest = 5000;
        };
      }
      # The programs read a forward as fields with spaces between them.
      { expose."a b" = 8080; }
      { expose."a;b" = 8080; }
      {
        reach.db = {
          host = "db.example -oProxyCommand=id";
          port = 5432;
          guest = 5432;
        };
      }
    ];
    expected = [
      false
      true
      true
      true
      true
      true
    ];
  };

  # The values that become names of files, text of the shell in the guest, or a path.
  testOptionRules = {
    expr = map refused [
      { name = "vm.1_a-b"; }
      { name = "a b"; }
      { name = "../vm"; }
      { name = "-vm"; }
      { hostName = "vm-1.example"; }
      { hostName = "vm_1"; }
      { hostName = "vm$(id)"; }
      { configDir = "nix/sandbox-vm"; }
      { configDir = "../sandbox-vm"; }
      { configDir = "nix/../../sandbox-vm"; }
      { configDir = "/etc"; }
      { configName = "vm; id"; }
      { backup.repository = "backups"; }
      { backup.repository = "~/backups"; }
      { backup.paths = [ "home" ]; }
      { logSize = 0; }
    ];
    expected = [
      false
      true
      true
      true
      false
      true
      true
      false
      true
      true
      true
      true
      true
      false
      true
      true
    ];
  };

  # The processes do not start with `devenv up` without a name.
  testProcesses = {
    expr =
      let
        config = host { options.backup.enable = true; };
      in
      [
        config.processes.sandbox-vm.start.enable
        config.processes.sandbox-vm-backup.start.enable
        config.processes.sandbox-vm.restart.on
        (config.tasks."sandbox-vm:ports".before)
        (lib.attrNames config.tasks)
      ];
    expected = [
      false
      false
      "never"
      [ "devenv:enterShell" ]
      [
        "sandbox-vm:backup"
        "sandbox-vm:home"
        "sandbox-vm:ports"
        "sandbox-vm:update"
      ]
    ];
  };
  testNoBackup = {
    expr =
      let
        config = host { };
      in
      [
        (config.processes ? sandbox-vm-backup)
        (config.tasks ? "sandbox-vm:backup")
        config.packages
      ];
    expected = [
      false
      false
      [ ]
    ];
  };
  testDisabled = {
    expr =
      let
        config = host { options.enable = lib.mkForce false; };
      in
      [
        config.processes
        config.tasks
      ];
    expected = [
      { }
      { }
    ];
  };

  # The command in the guest that writes the backup. The paths and the patterns are words
  # of the shell of the guest.
  testBackupCommand = {
    expr =
      let
        backup = (scripts {
          backup.enable = true;
          backup.paths = [
            "/home"
            "/var/lib/it's here"
            "/"
          ];
          backup.exclude = [
            "*/node_modules"
            "a b; id"
          ];
          backup.keep = {
            daily = 7;
            weekly = 0;
            monthly = 2;
          };
          backup.maxSize = 3;
        }).backup;
      in
      [
        (exports backup "SVM_BACKUP_COMMAND" (
          "sudo tar -c --one-file-system --numeric-owner '--exclude=*/node_modules' '--exclude=a b; id' -C / --"
          + " home 'var/lib/it'\\''s here' . || [ $? = 1 ]"
        ))
        (exports backup "SVM_BACKUP_KEEP" "--keep-daily 7 --keep-monthly 2")
        (exports backup "SVM_BACKUP_MAX_BYTES" "3221225472")
      ];
    expected = [
      true
      true
      true
    ];
  };
  # Without a size limit of the backup, the limit is the size of the disk.
  testBackupDefaultSize = {
    expr = exports (scripts {
      backup.enable = true;
      disk = 2;
    }).backup "SVM_BACKUP_MAX_BYTES" "2147483648";
    expected = true;
  };

  # The sizes in the units of the programs.
  testSizes = {
    expr =
      let
        vm = (scripts {
          memory = 2;
          logSize = 3;
          cpus = 5;
        }).vm;
      in
      [
        (exports vm "SVM_MEMORY_MIB" "2048")
        (exports vm "SVM_LOG_BYTES" "3145728")
        (exports vm "SVM_CPUS" "5")
        (exports vm "SVM_USER" "")
        (exports vm "SVM_INSTALLER_ISO" "")
        (exports vm "SVM_GUEST_MAC" constants.guestMac)
        (exports vm "SVM_PROXY_GATEWAY" constants.proxyGateway)
      ];
    expected = lib.replicate 7 true;
  };

  # The home-manager configuration: the home and update programs get it, and the sandbox-vm
  # process gets the home program only when it is set.
  testHomeManager =
    let
      ref = "git+https://example.com/dotfiles.git#me@vm";
      off = scripts { };
      on = scripts { homeManager = ref; };
    in
    {
      expr = [
        (exports off.vm "SVM_HOME_PROGRAM" "")
        (exports off.update "SVM_HOME_MANAGER" "")
        (exports on.vm "SVM_HOME_PROGRAM" (builtins.unsafeDiscardStringContext "${on.home}"))
        (exports on.home "SVM_HOME_MANAGER" ref)
        (exports on.update "SVM_HOME_MANAGER" ref)
      ];
      expected = lib.replicate 5 true;
    };

  # The proxy. Without options it permits all names, and only IPv4.
  testProxyDefault = {
    expr = [
      (proxySettings { })
      (acl { })
    ];
    expected = [
      {
        allow_missing_role = true;
        exit_timeout = "1s";
        network = "ip4";
        ip = "127.0.0.1";
      }
      {
        version = "v1";
        services = [ ];
        global_deny_list = [ ];
        default = {
          project = "devenv-sandbox-vm";
          action = "open";
        };
      }
    ];
  };
  testProxyAllowedDomains = {
    expr =
      (acl {
        allowedDomains = [ "github.com" ];
        allowedLocalDomains = [ "git.home.example" ];
        deniedDomains = [ "bad.example" ];
      })
      // {
        version = null;
        services = null;
      };
    expected = {
      version = null;
      services = null;
      global_deny_list = [ "bad.example" ];
      default = {
        project = "devenv-sandbox-vm";
        action = "enforce";
        allowed_domains = [
          "github.com"
          "git.home.example"
        ];
      };
    };
  };
  # An empty list permits no name. It is not the same as null.
  testProxyNoDomains = {
    expr = (acl { allowedDomains = [ ]; }).default;
    expected = {
      project = "devenv-sandbox-vm";
      action = "enforce";
      allowed_domains = [ ];
    };
  };
  testProxyRanges = {
    expr = proxySettings {
      ipv6 = true;
      denyRanges = [ "203.0.113.0/24" ];
      allowRanges = [ "10.1.0.0/16" ];
      allowedLocalDomains = [ "git.home.example" ];
    };
    expected = {
      allow_missing_role = true;
      exit_timeout = "1s";
      ip = "127.0.0.1";
      deny_ranges = [ "203.0.113.0/24" ];
      allow_ranges = [ "10.1.0.0/16" ];
      unsafe_ip_filter_bypassed_domains = [ "git.home.example" ];
    };
  };
  # proxy.settings has priority, but the proxy always listens only on the loopback address
  # and always uses the rules of this module.
  testProxySettings = {
    expr =
      let
        settings =
          (proxy {
            proxy.settings = {
              ip = "0.0.0.0";
              acl_file = "/tmp/acl.yaml";
              network = "ip";
              connect_timeout = "10s";
            };
          }).settings;
      in
      [
        settings.ip
        (lib.hasPrefix builtins.storeDir settings.acl_file)
        settings.network
        settings.connect_timeout
      ];
    expected = [
      "127.0.0.1"
      true
      "ip"
      "10s"
    ];
  };
}
