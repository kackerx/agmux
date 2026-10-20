"""T66 npm publishes with Trusted Publishing (OIDC): the npm job of .github/workflows/release.yml has `permissions: id-token: write` (and contents: read for gh release download), Node 24 (npm >= 11.5.1), `npm publish ... --provenance`,
and no token: no NPM_TOKEN / NODE_AUTH_TOKEN anywhere in the workflow; the other jobs keep what they had. Only the files are read here (YAML parsed with the system Ruby, text checks): the OIDC exchange itself can only be
seen on the first tag after the Trusted Publisher is set up on npmjs.com. Spec: specs/m3-config/tasks.md「第十三批」T66."""
import json, re, shutil, subprocess, unittest
from lib import *

WF = SRC / ".github" / "workflows" / "release.yml"
RUBY = shutil.which("ruby")


def load(path):
    r = subprocess.run([RUBY, "-ryaml", "-rjson", "-e", "puts JSON.generate(YAML.load(File.read(ARGV[0])))", str(path)], capture_output=True, text=True); assert r.returncode == 0, r.stderr; return json.loads(r.stdout)


@unittest.skipUnless(RUBY, "needs ruby to parse the YAML")
class Release(unittest.TestCase):
    def setUp(self): self.wf = load(WF); self.text = WF.read_text(); self.npm = self.wf["jobs"]["npm"]

    def test_the_workflow_is_valid_yaml_with_the_four_jobs_in_order(self):
        self.assertEqual(list(self.wf["jobs"]), ["ci", "release", "tap", "npm"]); self.assertEqual(self.npm["needs"], "release"); self.assertEqual(self.wf["name"], "release")

    def test_the_npm_job_may_ask_for_an_oidc_token_and_still_read_the_release(self):
        self.assertEqual(self.npm["permissions"], {"contents": "read", "id-token": "write"})

    def test_no_job_but_npm_gets_the_oidc_permission(self):
        for name, job in self.wf["jobs"].items():
            if name != "npm": self.assertNotIn("id-token", job.get("permissions", {}) or {}, name)
        self.assertNotIn("id-token", self.wf.get("permissions", {}) or {}, "not at the workflow level either"); self.assertEqual(self.wf["jobs"]["release"]["permissions"], {"contents": "write"})

    def test_there_is_no_npm_token_anywhere(self):
        for word in ("NPM_TOKEN", "NODE_AUTH_TOKEN", "_authToken", "npm_token"): self.assertNotIn(word, self.text, word)
        self.assertNotIn("NPM_TOKEN", (SRC / "README.md").read_text()); self.assertIn("secrets.TAP_GITHUB_TOKEN", self.text, "the tap job still has its own token")

    def test_node_24_with_the_npm_registry(self):
        steps = self.npm["steps"]; node = next(s for s in steps if str(s.get("uses", "")).startswith("actions/setup-node")); self.assertEqual(str(node["with"]["node-version"]), "24"); self.assertEqual(node["with"]["registry-url"], "https://registry.npmjs.org")
        self.assertNotIn("env", node, "no token handed to setup-node")

    def test_every_npm_publish_has_provenance_and_public_access_and_a_dot_slash_path(self):
        pubs = [l.strip() for s in self.npm["steps"] for l in str(s.get("run", "")).splitlines() if "npm publish" in l]; self.assertEqual(len(pubs), 2, pubs)
        for l in pubs:
            self.assertIn("--provenance", l); self.assertIn("--access public", l); self.assertRegex(l, r'npm publish "?\./', "a bare out/x is a GitHub user/repo to npm")
        self.assertTrue(any("./out/agmux --access" in l for l in pubs), "the shell package"); self.assertTrue(any('"./$d"' in l for l in pubs), "the platform packages in a loop")

    def test_the_steps_before_the_publish_are_as_they_were(self):
        runs = "\n".join(str(s.get("run", "")) for s in self.npm["steps"]); self.assertIn('gh release download "$GITHUB_REF_NAME" --pattern \'*.tar.gz\' --dir archives', runs); self.assertIn('node npm/build.mjs "$GITHUB_REF_NAME" archives out', runs)
        self.assertIn('out/agmux-linux-x64/bin/agmux --version | grep -F "agmux $GITHUB_REF_NAME "', runs, "the binary says the tag before anything is published"); self.assertLess(runs.index("--version | grep -F"), runs.index("npm publish"))
        gh = next(s for s in self.npm["steps"] if "gh release download" in str(s.get("run", ""))); self.assertEqual(gh["env"], {"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}"})

    def test_the_shell_package_is_published_last(self):
        run = next(s["run"] for s in self.npm["steps"] if "npm publish" in str(s.get("run", ""))); self.assertGreater(run.index("npm publish ./out/agmux --access"), run.index("for d in out/agmux-"), "platform packages first: the shell's optionalDependencies must exist")

    def test_the_package_json_files_have_a_repository_npm_accepts(self):
        for p in (SRC / "npm" / "agmux" / "package.json",):
            d = json.loads(p.read_text()); repo = d["repository"]; url = repo if isinstance(repo, str) else repo.get("url", ""); self.assertIn("kackerx/agmux", url, "the Trusted Publisher is configured for the repository kackerx/agmux")
        b = (SRC / "npm" / "build.mjs").read_text(); self.assertIn("repository: shell.repository", b, "the platform packages say the same repository")


if __name__ == "__main__":
    unittest.main()
