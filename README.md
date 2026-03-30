[English](README.md) | [Русский](README.ru.md)

# FLibGoLite-Al — An OPDS server that will run even on your Keenetic (aarch64)

**FLibGoLite-Al** is an optimized fork of the original project, specifically adapted for stable operation on Keenetic routers and other resource-constrained embedded systems.

**Warning:** This fork is not compatible with the original project in terms of configuration files, localization, and database!
**Warning:** To run on Keenetic routers, be sure to read the "Installation on Keenetic" section!

Original project and full documentation:
* [vinser/flibgolite](https://github.com/vinser/flibgolite)
* [Official Documentation](https://vinser.github.io/flibgolite-docs/en/docs/user-guide/)

### Features
* **Recursive scanning** of all subdirectories.
* **File system monitoring:** physically deleted files are automatically removed from the database, and new ones are indexed.
* Supported formats: **fb2, fb2.zip, fb3, epub, prc, mobi, azw, azw3, pdf** (metadata extraction for PDF format is extremely poorly supported).
* **Reading series:** all series are extracted from fb2, epub (including `calibre:series`), and fb3.
* Excellent **genre cataloging**, inherited from the original.
* **Tracking new arrivals** by authors, genres, series, and languages.
* **Folder mode:** if you do not trust cataloging tools, you can organize a convenient folder system in the main scanning directory, and the library will serve books based on their location in the file system.
* **Genre filtering:** allows you to filter out unnecessary items during scanning. If you only need sci-fi or detectives, you don't have to store the rest of the books in the DB and serve them to clients. *Note:* This is only useful if your library consists of fb2/fb3 files. In other supported formats, genres are usually not specified or contain arbitrary text.
* **Language filtering:** similar filtering during scanning. If you only need books in Russian, the rest will be ignored. *Note:* relevant only for fb2/fb3/epub/mobi formats. Language detection in PDF files is highly conditional.
* **Resource management:** implemented a semaphore system to limit CPU and RAM usage during book scanning, cover generation, and file conversion.
* **DB optimization:** improved SQLite performance and configuration for smooth processing of huge libraries (hundreds of thousands of books) on low-power hardware.
* **Localization rework:** book delivery is no longer tied to the interface language, ensuring consistent navigation across different OPDS clients.
* **High speed:** scanning a library of ~700k books takes about 2 hours on a Keenetic 1012 router. The test library consisted of fb2 files packed in several hundred archives, along with ~30k epub and mobi files. On a fanless PC with an i5-4200U, the same library is scanned in 20 minutes.

---

### Installation on all supported devices (except Keenetic)

1. Download the executable file for your OS and CPU architecture.
2. Configure the startup using the following sequence of commands:

   *As administrator on a Windows PC:*
   ```cmd
   flibgolite.exe -service install
   flibgolite.exe -service start
   flibgolite.exe -service status
   ```

   *Or on MacOS, Linux, FreeBSD:*
   ```bash
   sudo ./flibgolite -service install
   sudo ./flibgolite -service start
   sudo ./flibgolite -service status
   ```

3. After the first launch, the program will create all service directories. Find the `config-al.yml` file in the `config` directory and configure the path to your books folder. Nested directories are supported.
   > *Example:* `STOCK: "D:/Books"`
4. Restart the service — the program will start scanning and will be immediately available for use.
5. In your e-reader application, configure a new OPDS catalog (server) using the following address:
   `http://192.168.1.1:8087/opds`
   > *Note: Replace `192.168.1.1` with the actual IP address of your computer/server if it is different.*

### Installation on Keenetic

A correctly configured **Entware** environment is required on your router.

1. Download the `flibgolite-al-keenetic.tar` archive and the `S99flibgolite` startup script.
2. Copy `flibgolite-al-keenetic.tar` to the `/opt/bin/` directory on your router.
3. Unpack the archive (this will extract the executable and the configuration file):
   ```bash
   tar -xpvf /opt/bin/flibgolite-al-keenetic.tar -C /opt/
   ```
4. As a precaution, explicitly set execution permissions for the extracted binary:
   ```bash
   chmod +x /opt/bin/flibgolite
   ```
5. Open the extracted configuration file (`/opt/bin/config/config-al.yml`), locate the `STOCK:` line under the `library:` section, and enter the exact path to your book directory.
   > *Example:* `STOCK: "/tmp/mnt/YOUR_DISK_ID/Books"`
6. Copy the startup script `S99flibgolite` to the `/opt/etc/init.d/` directory.
7. Set execution permissions for the startup script:
   ```bash
   chmod +x /opt/etc/init.d/S99flibgolite
   ```
8. Start the service:
   ```bash
   /opt/etc/init.d/S99flibgolite start
   ```
9. Wait for 60 seconds and verify that the process is running successfully by using the command:
   ```bash
   ps | grep flibgolite
   ```
10. In your e-reader application, configure a new OPDS catalog (server) using the following address:
    `http://192.168.1.1:8087/opds`
    > *Note: Replace `192.168.1.1` with your router's actual IP address if it is different.*

Scanning will begin in approximately 60 seconds. Once indexing is complete, your library will be ready to use.

---

### Compatibility and system requirements

* **Tested on:** Keenetic KN-1012
* **Verified clients:** AlReaderX, FBReader, Cool Reader.
* **Important note:** For stable operation when indexing large libraries, it is highly recommended to create a **SWAP partition** on the drive with Entware!

---

**This is an independent fork aimed at maximum performance in resource-constrained environments.**

*Suggestions and bug reports are welcome in the [Issues](https://github.com/alanneverland/flibgolite-keenetic-aarch64/issues) section of this repository.*