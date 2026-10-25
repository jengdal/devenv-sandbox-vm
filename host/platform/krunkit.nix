# The hypervisor: the packages krunkit and libkrun-efi of nixpkgs, with these changes:
#
# - libkrun has a fixed version. nixpkgs does not update it.
# - libkrun has a patch: the VM gives its free memory back to macOS
#   (./libkrun-balloon-macos.patch, a backport of libkrun PR #931).
# - The VM has no GPU device. libkrun has no GPU device and no libraries of the GPU, and
#   krunkit does not give a GPU device to each VM.
#
# krunkit has the version of nixpkgs. The build fails if the code that postPatch removes
# changes. Then change the text in postPatch.
#
# To update libkrun to a new version: change `version` and the two hashes, and make sure that
# the patch applies and that a VM gives its free memory back (docs/src/memory.md). Remove the patch
# when a 1.x release of libkrun has PR #931 and enables it for krunkit.
{ pkgs }:
let
  inherit (pkgs) fetchFromGitHub rustPlatform;

  version = "1.19.7";

  src = fetchFromGitHub {
    owner = "libkrun";
    repo = "libkrun";
    tag = "v${version}";
    hash = "sha256-VtepY0sAqP18OIhIPwESoGi7E2Ahs4ihJjLyLH78JAM=";
  };

  # The init program that libkrun contains. nixpkgs builds it from its own version of the
  # source, thus it is built again here. From the package libkrun-efi of nixpkgs
  # (pkgs/by-name/li/libkrun-efi/package.nix, MIT license, notice in NOTICE).
  initBinary = pkgs.buildPackages.pkgsCross.aarch64-multiplatform.pkgsStatic.stdenv.mkDerivation {
    pname = "libkrun-init";
    inherit version src;

    dontConfigure = true;

    buildPhase = ''
      runHook preBuild
      cd src/init_blob/init
      $CC -O2 -static -Wall -o init init.c dhcp.c
      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      install -D init $out/init
      runHook postInstall
    '';
  };

  libkrun-efi = pkgs.libkrun-efi.overrideAttrs (old: {
    inherit version src;

    cargoDeps = rustPlatform.fetchCargoVendor {
      inherit src;
      hash = "sha256-Ab7b37Npf0vNg7rF8KfPdLYCBuBCyRAYhR13RLecV04=";
    };

    # Gives the free memory of the VM back to macOS.
    patches = (old.patches or [ ]) ++ [ ./libkrun-balloon-macos.patch ];

    # No libraries of the GPU.
    buildInputs = [ ];

    makeFlags = [
      "PREFIX=${placeholder "out"}"
      "EFI=1"
      # No GPU device.
      "GPU=0"
    ];

    env = old.env // {
      KRUN_INIT_BINARY_PATH = "${initBinary}/init";
    };
  });
in
(pkgs.krunkit.override { inherit libkrun-efi; }).overrideAttrs (old: {
  buildInputs = [ libkrun-efi ];

  # Removes the GPU device that krunkit gives to each VM.
  postPatch = (old.postPatch or "") + ''
    substituteInPlace src/context.rs \
      --replace-fail \
        '    fn krun_set_gpu_options2(ctx_id: u32, virgl_flags: u32, shm_size: u64) -> i32;
    ' \
        "" \
      --replace-fail \
        'const VIRGLRENDERER_VENUS: u32 = 1 << 6;
    const VIRGLRENDERER_NO_VIRGL: u32 = 1 << 7;
    ' \
        "" \
      --replace-fail \
        '        // Temporarily enable GPU by default
            let virgl_flags = VIRGLRENDERER_VENUS | VIRGLRENDERER_NO_VIRGL;
            let sys = sysinfo::System::new_all();
            // Limit RAM + VRAM to 64 GB (36 bit IPA address limit) minus 2 GB (start address plus rounding).
            let rounded_mem = ((args.memory as u64) / 1024 + 1) * 1024;
            let vram = std::cmp::min((63488 - rounded_mem) * 1024 * 1024, sys.total_memory());
            if unsafe { krun_set_gpu_options2(id, virgl_flags, vram) } < 0 {
                return Err(anyhow!("unable to set krun vCPU/RAM configuration"));
            }
    ' \
        ""
  '';
})
