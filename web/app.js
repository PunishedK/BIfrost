
(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);

  const params = new URLSearchParams(location.search);
  const code = params.get("code");
  const isPair = location.pathname === "/pair";

  // Remove invitation code from browser URL.
  history.replaceState(null, "", location.pathname);

  $("pair-view").hidden = !isPair;
  $("app-view").hidden = isPair;

  const MAX_FILE_SIZE = 512 * 1024 * 1024;

  function msg(text) {
    const el = $("msg");
    el.textContent = text;
    el.hidden = !text;
  }

  function fmt(bytes) {
    if (bytes < 1024) {
      return `${bytes} B`;
    }

    if (bytes < 1048576) {
      return `${(bytes / 1024).toFixed(1)} KiB`;
    }

    return `${(bytes / 1048576).toFixed(1)} MiB`;
  }

  async function request(url, opts = {}) {
    const response = await fetch(url, {
      ...opts,
      cache: "no-store",
      credentials: "same-origin"
    });

    if (!response.ok) {
      const body = await response
        .json()
        .catch(() => ({}));

      throw new Error(
        body.error || `HTTP ${response.status}`
      );
    }

    return response;
  }

  // ----------------------------------------
  // PAIRING
  // ----------------------------------------

  if (isPair) {
    const pairButton = $("pair-btn");
    const pairMessage = $("pair-msg");

    pairButton.addEventListener("click", async () => {
      pairButton.disabled = true;
      pairButton.textContent = "Connecting…";
      pairMessage.textContent = "";

      try {
        if (!code) {
          throw new Error(
            "Invitation missing. Generate a new QR code on the host device."
          );
        }

        await request("/api/pair/redeem", {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify({ code })
        });

        pairButton.textContent = "Connected";

        location.replace("/");

      } catch (error) {
        pairMessage.textContent = error.message;

        pairButton.disabled = false;
        pairButton.textContent = "Try connecting again";
      }
    });

    return;
  }

  // ----------------------------------------
  // FILE LIST
  // ----------------------------------------

  async function load() {
    const parent = $("file-items");
    const count = $("file-count");

    parent.replaceChildren();

    try {
      const response = await request("/api/files");
      const data = await response.json();

      count.textContent =
        `${data.length} stored file${data.length === 1 ? "" : "s"}`;

      if (!data.length) {
        const empty = document.createElement("p");

        empty.className = "empty";
        empty.textContent =
          "The vault is empty. Send your first file across the bridge.";

        parent.append(empty);
      }

      for (const item of data) {
        const row = document.createElement("div");
        row.className = "file";

        // File details
        const details = document.createElement("div");
        details.className = "details";

        const name = document.createElement("strong");
        name.textContent = item.name;

        const size = document.createElement("small");
        size.textContent = fmt(item.size);

        details.append(name, size);

        // Action buttons
        const actions = document.createElement("div");
        actions.className = "actions";

        const download = document.createElement("button");
        download.type = "button";
        download.textContent = "↓ Download";

        download.addEventListener("click", () => {
          location.href =
            `/api/files/${encodeURIComponent(item.id)}/download`;
        });

        const remove = document.createElement("button");
        remove.type = "button";
        remove.className = "danger";
        remove.textContent = "Delete";

        remove.addEventListener("click", async () => {
          if (!confirm(`Delete ${item.name}?`)) {
            return;
          }

          remove.disabled = true;
          remove.textContent = "Deleting…";

          try {
            await request(
              `/api/files/${encodeURIComponent(item.id)}`,
              { method: "DELETE" }
            );

            await load();

          } catch (error) {
            msg(error.message);

            remove.disabled = false;
            remove.textContent = "Delete";
          }
        });

        actions.append(download, remove);
        row.append(details, actions);
        parent.append(row);
      }

      msg("");

    } catch (error) {
      const notPaired =
        error.message === "pair with BIfrost first";

      msg(
        notPaired
          ? "This device is not paired. Scan a new QR code from the host device."
          : error.message
      );
    }
  }

  // ----------------------------------------
  // REFRESH
  // ----------------------------------------

  $("refresh-btn").addEventListener("click", load);

  // ----------------------------------------
  // UPLOAD FILES
  // ----------------------------------------

  $("file-picker").addEventListener(
    "change",
    async (event) => {
      const selected = Array.from(
        event.target.files || []
      );

      for (const file of selected) {
        const row = document.createElement("div");
        row.className = "file";

        const details = document.createElement("div");
        details.className = "details";

        const name = document.createElement("strong");
        name.textContent = file.name;

        const bar = document.createElement("div");
        bar.className = "bar";

        const fill = document.createElement("div");
        fill.className = "fill";

        bar.append(fill);

        const state = document.createElement("small");
        state.textContent = "Waiting…";

        details.append(name, bar, state);
        row.append(details);

        $("upload-items").append(row);

        if (file.size > MAX_FILE_SIZE) {
          state.textContent =
            "File exceeds the 512 MiB limit";
          continue;
        }

        try {
          await new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();

            xhr.open(
              "POST",
              `/api/files?name=${encodeURIComponent(file.name)}`
            );

            xhr.upload.onprogress = (event) => {
              if (event.lengthComputable) {
                const percent = Math.round(
                  (event.loaded / event.total) * 100
                );

                fill.style.width = `${percent}%`;
                state.textContent =
                  `Crossing the bridge… ${percent}%`;
              }
            };

            xhr.onload = () => {
              if (xhr.status === 201) {
                resolve();
              } else {
                reject(
                  new Error(
                    `Upload failed: HTTP ${xhr.status}`
                  )
                );
              }
            };

            xhr.onerror = () => {
              reject(new Error("Connection lost"));
            };

            xhr.send(file);
          });

          state.textContent = "Uploaded successfully";
          fill.style.width = "100%";

        } catch (error) {
          state.textContent = error.message;
        }
      }

      event.target.value = "";

      await load();
    }
  );

  // Initial vault refresh
  load();

})();
