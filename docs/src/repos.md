# How to put your repos in the VM

A new VM has an empty home directory. Pick the option that gives the VM only the access that you are
comfortable with. Each option works for more than one repo. You can use different options for
different repos.

## Option 1: No credentials in the VM

The VM gets a bare repo. The host and the VM push to this repo and fetch from it. The VM cannot
connect to your forge. This option is the most secure. But it is also the least convenient: in the
VM, you cannot push to your forge or pull from it.

1. On the host, in the repo, run these commands:

   ```bash
   ssh devenv-<project> git init --bare -b main repos/<repo>.git
   git remote add sandbox-vm devenv-<project>:repos/<repo>.git
   git push sandbox-vm main
   ```

2. In the VM, run this command:

   ```bash
   git clone ~/repos/<repo>.git
   ```

After that, work moves as follows:

| Direction  | Procedure                                                                          |
| ---------- | ---------------------------------------------------------------------------------- |
| Host to VM | On the host: `git push sandbox-vm <branch>`. Then in the VM: `git pull`.           |
| VM to host | In the VM: `git push`. Then on the host: `git fetch sandbox-vm`. Review and merge. |

Use the name of your branch where the example shows `main`. The `-b` option sets the default branch
of the bare repo. `git clone` checks out this branch.

Use merge commits when you pull, in the VM and on the host:

```bash
git config pull.rebase false
```

## Option 2: A deploy key

The VM clones from your forge and pushes to your forge directly. The VM authenticates with a deploy
key. A deploy key gives access only to the repos that you add it to, not to all repos of your
account.

The steps are for GitHub. GitHub accepts a deploy key on only one repo, so each repo gets its own
key and a host alias. `<repo>` is the name of the repo on GitHub.

1. In the VM, create a key for the repo:

   ```bash
   ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ed25519_<repo>
   cat ~/.ssh/id_ed25519_<repo>.pub
   ```

2. Add the public key to the repo as a deploy key. Give write access only if the VM must push.
3. In the VM, add a host alias for the key:

   ```bash
   mkdir -p ~/.ssh/config.d
   cat > ~/.ssh/config.d/<repo> <<'EOF'
   Host github-<repo>
     HostName github.com
     User git
     IdentityFile ~/.ssh/id_ed25519_<repo>
     IdentitiesOnly yes
   EOF
   ```

   The VM reads all files in `~/.ssh/config.d`. This also works when Home Manager manages
   `~/.ssh/config`.

4. Connect once to add GitHub to `~/.ssh/known_hosts`. Before you accept the key, compare its
   fingerprint with [GitHub's SSH key
   fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints):

   ```bash
   ssh -T github-<repo>
   ```

5. Clone with the host alias:

   ```bash
   git clone github-<repo>:<owner>/<repo>.git
   ```

WARNING: Be careful with the access that you give.

- A key with write access can push all branches. Protect the important branches. Then changes get to
  these branches only through a pull request that a person approved.
- If the repo has CI, a pushed branch can start CI. CI then runs with the secrets that it has.
- Use a deploy key. Do not use a key of your account. An account key gives access to all repos that
  you can access.

On other forges:

- **Forgejo** deploy keys can also be read-only or have write access. You can add one key to more
  than one repo, so a host alias is optional. Without an alias, clone with the SSH URL of the repo.
- **Gitea** is almost the same as Forgejo. Test it with your version.

## Option 3: A mixture

Example: a read-only deploy key and a bare repo. The VM pulls from your forge with the key. Work
goes out through the bare repo, as in option 1. Then the VM never pushes to your forge.

## Other options

You can also copy files in and out of the VM with rsync, and commit on the host. Use what fits your
workflow and the risks that you accept.
