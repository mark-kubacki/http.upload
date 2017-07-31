This template is meant to help you with the structure.  
Please remove the following lists, retaining the essence of the one or two points that actually apply.
In any case, do not skip sections marked as obligatory.

No links to external resources, please.

Your issue will be closed for "formal" reasons for not following the structure of this template:

### Submission Type (obligatory)

One or two sentences what this is about. If in doubt ask questions on the forums or send the author(s) an email.

  - [ ] Bug Report (something does not work; »This is a bug report concerning A.«)
  - [ ] Flaw or Request for Enhancement (RFE) (»Please support B or cover edge case C.«)
  - [ ] Request for Assistance, or Usage Notice (»I use it here…, please prioritize changes that align with my work.«)
  - [ ] Intent to Contribute (»Would you entertain a PR for new fearure D?«)

### Submitter (obligatory)

To get an idea of who champions this, and to get an idea of your use case.

  - [ ] I just use Caddy for my personal site.
  - [ ] … or commercial context.
  - [ ] This is for a derivative work, which is available free of charge (not necessarily open-source) here….
  - [ ] … derivative work, which is or will be sold.

  - [ ] I have sent you a bounty meant for this issue. – https://paypal.me/mkii  
    (Even if no remedy can be found, this counts as work: triage, consideration, research, (re-)design, implementation, verfication, validation, QA.)

### Environment

You can skip details about the Environment on Requests for Enhancement.
Even if a bug applies to more than one Environment, pick one representative and give details for it consistently.

 - architecture (amd64, x86, `uname -mpi`)
 - Linux, Windows, …
 - version of Windows (win+r → `cmd` → `ver`) or Linux (`uname -rv` without the *datetime* please)
 - filesystem you upload to (find in the output of command `mount` the right part of `to …`)
 - user and group ID *caddy* runs as, and any retained ambient capabilities (if you know them; see your *systemd service file* if you use one)
 - the plugin version (enable logging, use the line that looks like this: `12345678-09abcdef-…`)

### Configuration File

```ini
Please paste the relevant part of your configuration file.
```

### Expected Behaviour

### Unexpected Result

### Eigenleistung

Describe what you have tried to mitigate or debug the above.

  - Try the latest version of Linux and ext4. Any differences?
  - Does it work on architecture `amd64`/`x86_64` and/or `i686`?
  - Does it work outside of *Docker* and/or *rkt* and/or *systemd-nspawn* and/or *Hyper*?
