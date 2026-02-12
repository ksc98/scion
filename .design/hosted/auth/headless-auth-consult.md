In Linux, detecting the presence of a web browser or a graphical environment is
less about finding a specific binary and more about assessing the **execution
context**. Since "no browser" usually implies a headless server or a restricted
shell, you can use several layers of detection.

---

## 1. Environment Variable Checks (The Standard Way) {:#1.-environment}

The most common way to determine if a GUI is available (and thus if a browser
can be launched) is to check for the $DISPLAY or $WAYLAND_DISPLAY variables.

- **$DISPLAY**: Used by the X Window System. If this is empty, no X17-based
  graphical applications can open.
- **$WAYLAND_DISPLAY**: Used by modern Wayland compositors.

**Bash Script Example**:

```
if [ -z "$DISPLAY" ] && [ -z "$WAYLAND_DISPLAY" ]; then
    echo "No graphical display detected (Headless)."
else
    echo "Graphical display available."
fi
```

## 2. Probing for Browser Binaries {:#2.-probing}

If you want to know if a browser is _installed_ (regardless of whether it can
run), you should check the system PATH for common binaries.

```
# Check for common browser executables
browsers=(google-chrome chrome chromium firefox brave safari microsoft-edge)
for b in "${browsers[@]}"; do
    if command -v "$b" >/dev/null 2>&1; then
        echo "$b is installed."
    fi
done
```

## 3. Using xdg-settings or xdg-mime {:#3.-using}

To see what the system considers the default browser, you can query the XDG
(Cross-Desktop Group) utilities. If these commands fail or return nothing, the
system likely isn't configured for graphical web interaction.

```
# Check default browser
xdg-settings get default-web-browser

# Check what handles https links
xdg-mime query default x-scheme-handler/https
```

## 4. Identifying the TTY/Session Type {:#4.-identifying}

You can detect if the user is in a physical terminal (TTY) versus a virtual
terminal inside a GUI.

- **tty command**: Returns the name of the connected terminal. /dev/tty1-6
  usually indicates a physical console (no GUI), while /dev/pts/* indicates a
  terminal emulator or SSH session.
- **loginctl**: Can show if the session is tty or x11/wayland.

```
loginctl show-session $(loginctl | grep $(whoami) | awk '{print $1}') -p Type
```
---

## Technical Summary for Automation {:#technical-summary}

If you are writing a script that needs to decide between a **Local Web Server**
flow and a **Device Code** flow, the most robust check is a combination of
$DISPLAY and checking if the stdout is a TTY:


```
if [[ -t 1 ]] && [[ -z "$DISPLAY" ]]; then
    # Interactive terminal but no GUI
    echo "Falling back to Device Code flow..."
fi
```


_Note: Even if a browser binary is present, it will fail to launch in a headless
environment unless a virtual framebuffer like Xvfb is running_.

Once you have determined that there is not a browser, alter the oauth login flow
to use device flow as described below

---

## For CLI Applications (Device Flow) {:#cli-applications}

If you are developing a tool and need to implement this yourself, you should use
the **OAuth 2.0 Device Authorization Grant** (defined in RFC 8628).

### Implementation Steps {:#implementation-steps}

1. **Request Device Code**: Your CLI sends a POST request to
  https://oauth2.googleapis.com/device/code with your client_id and scopes.
1. **Display Instructions**: The response returns a device_code, a user_code,
  and a verification_url. Your CLI displays: "Go to
  [google.com/device](https://google.com/device){:.external} and enter code:
  ABCD-1234"
1. **Polling**: While the user is typing the code into their phone or laptop,
  your CLI polls the token endpoint (https://oauth2.googleapis.com/token) every
  few seconds.
1. **Token Exchange**: Once the user approves the request in their browser, the
  next poll will return the access_token and refresh_token.

### Legacy "OOB" Note {:#legacy-"oob"}

Historically, developers used urn:ietf:wg:oauth:2.0:oob (Out-of-Band) as a
redirect URI. **Google has deprecated this** for most account types due to
security risks. If you are starting a new project, use the **Device Flow**
described above or a **Local Loopback** (if the CLI is on a machine that _has_ a
browser).

