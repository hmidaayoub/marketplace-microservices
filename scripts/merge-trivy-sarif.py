#!/usr/bin/env python3
"""
Merges several Trivy SARIF reports into one, for upload as a single analysis.

The eight image scans in the stack job each produce their own file, and every one of
them describes a run whose tool is "Trivy" with no automationDetails. Uploaded as a
directory that is eight runs which all derive the same code-scanning category, and the
CodeQL action rejects the lot:

    The CodeQL Action does not support uploading multiple SARIF runs with the same
    category. Please update your workflow to upload a single run per category.

The alternative to merging is eight upload steps with eight explicit categories. This
is here instead, and it carries the image name into each result's message so that
combining them loses nothing.

That last part is not decoration. Only some results say where they came from on their
own: an OS package finding is located at library/marketplace-microservices-frontend,
but a Python or Java one is located at "Python" or at a jar path, which is the same
string for every image that ships that ecosystem. Merged without a label, those become
findings the Security tab cannot attribute to an image.

    merge-trivy-sarif.py OUT.sarif IN.sarif [IN.sarif ...]

The label comes from the input filename - trivy-image-frontend.sarif labels its results
"frontend" - because that is the name the workflow already gave the file, and passing
the image a second time on the command line is a second place for it to disagree.

Rules are deduplicated by id, because the same CVE in the same base image is reported
once per image that shares it. results[].ruleIndex is renumbered against the merged
rule list rather than carried over - the indices are positional, so copying them from
eight separate arrays is how a finding ends up displayed under another finding's title.
"""

import json
import os
import sys


def label_for(path: str) -> str:
    """"frontend", from a path like sarif/trivy-image-frontend.sarif."""
    name = os.path.basename(path)
    for prefix in ("trivy-image-", "trivy-"):
        if name.startswith(prefix):
            name = name[len(prefix):]
            break
    return name.removesuffix(".sarif")


def main(argv: list[str]) -> int:
    if len(argv) < 3:
        sys.exit(__doc__.strip())

    out_path, in_paths = argv[1], argv[2:]

    rules: dict[str, dict] = {}
    results: list[dict] = []
    driver = {"name": "Trivy"}

    for path in in_paths:
        with open(path) as handle:
            report = json.load(handle)

        label = label_for(path)

        for run in report.get("runs") or []:
            run_driver = run.get("tool", {}).get("driver", {})
            # Keep the first run's identifying fields - name, version, informationUri -
            # rather than the last one's. They are identical across these files by
            # construction, and an assertion here would fail on a Trivy upgrade
            # mid-workflow for no benefit.
            for key in ("name", "version", "informationUri", "semanticVersion"):
                if key in run_driver and key not in driver:
                    driver[key] = run_driver[key]

            for rule in run_driver.get("rules") or []:
                rules.setdefault(rule["id"], rule)

            for result in run.get("results") or []:
                # Dropped here and recomputed below: it indexes into the run's own rule
                # array, which this merge replaces.
                result.pop("ruleIndex", None)

                # The label goes in the message rather than only in properties, because
                # the message is what the Security tab shows in the list. A result whose
                # location is "Python" is otherwise indistinguishable from the same
                # finding in the other Python image.
                message = result.setdefault("message", {})
                if "text" in message and not message["text"].startswith(f"[{label}]"):
                    message["text"] = f"[{label}] {message['text']}"
                result.setdefault("properties", {})["image"] = label

                results.append(result)

    ordered = list(rules.values())
    index_of = {rule["id"]: position for position, rule in enumerate(ordered)}

    for result in results:
        rule_id = result.get("ruleId")
        if rule_id in index_of:
            result["ruleIndex"] = index_of[rule_id]

    driver["rules"] = ordered

    merged = {
        "version": "2.1.0",
        "$schema": "https://json.schemastore.org/sarif-2.1.0-rtm.5.json",
        "runs": [{"tool": {"driver": driver}, "results": results}],
    }

    with open(out_path, "w") as handle:
        json.dump(merged, handle, indent=2)

    print(f"merged {len(in_paths)} report(s): {len(results)} result(s), {len(ordered)} rule(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
