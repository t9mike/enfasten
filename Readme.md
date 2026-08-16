# Enfasten ⚡️
### Automatically make your site faster with optimized responsive images

Enfasten is a tool written in Go which takes in a static site, scales the images down to a number of different sizes, then rewrites all of your HTML to use [responsive image tags](https://developer.mozilla.org/en-US/docs/Learn/HTML/Multimedia_and_embedding/Responsive_images) with `srcset` attributes. It can also run all of your images through an optimizer like [ImageOptim](https://imageoptim.com/mac).
This makes your static site faster for people to load because [their browser](http://caniuse.com/#feat=srcset) can load the best image for their screen size and resolution, and it's backwards compatible with older browsers.

Even though they save tons of bandwidth, few sites use optimized responsive images because it requires creating a custom image processing pipeline that has to be incremental in order to achieve good performance. Enfasten solves this problem by doing everything for you!

I wrote this because [on my site](http://thume.ca/) I frequently include images which are absurd sizes, such as a `2146x1258` screenshot from a high-DPI display. The user's browser downloads this huge image and promptly resizes it down to fit in my 660px wide blog. This is especially bad when people without high-DPI displays visit my site, but I still want things to look nice on high-DPI displays. The fact that I don't always remember to [optimize my images](https://blog.codinghorror.com/zopfli-optimization-literally-free-bandwidth/) makes this waste even worse.

With Enfasten, no matter what static site generator you use, you can run your site through Enfasten before you deploy and get **free bandwidth savings!!**

## Features

Enfasten has a bunch of features that would take a long time to replicate in a script or [Gulp](https://gulpjs.com/) task you set up yourself:

- **Incremental**: Enfasten will only spend time loading, resizing and optimizing new images. This is especially important when using an optimizer since optimization can take a *long* time. Optimizing all my site's images every time I built it would take an hour.
- **Stable outputs**: Unchanged copied files, transformed HTML, and manifests are left untouched so timestamp-based deployment tools such as `rsync` only transfer real updates.
- **Source-only HTML includes**: Exact `<!-- include fragment.html -->` directives expand before HTML transformation. Enfasten searches the nearest configured include folder and then each parent, making locale or section overrides easy while preserving a shared fallback; include folders are never published.
- **Cache-friendly renaming**: Enfasten will put all your images in one folder and add hashes to their name so that you can tell your CDN/browsers to cache them indefinitely without invalidation issues.
- **Fast**: It's not ridiculously optimized, but a build of my site with no new images takes `0.36s`, plus it only processes new images when necessary and uses a fast resizer.
- **Culling**: Sometimes PNG optimizers can lead to images with larger dimensions having smaller file size than ones with smaller dimensions because of how compression interacts with resizing filters. Enfasten has a special mode to detect these and cull the inefficient downscaled images.
- Only resizes when it's worth it: If you have an image that is `1345px` wide and you tell Enfasten to make a version that is `1320px` wide, by default it won't bother. You can configure the "close enough" threshold if you want. For `.jpg` file the threshold for this is separate since resizing JPEG files often comes with a quality hit because of re-encoding, so small changes in size really aren't worth it.
- Only downscaling: unlike what a simpler script could do, Enfasten actually checks the size of your images against the widths you want, and only ever resizes an image *down*. If there's no smaller sizes in your size list, then Enfasten won't even add a `srcset` attribute to the image tags.
- Proper copying: When copying and transforming files from your input folder to your output folder, Enfasten will make sure to delete files in the output folder that shouldn't be there anymore.
- Keeps originals: Your originals are copied, hashed, losslessly optimized, and offered in srcset attributes, because why not. This also enables the features of avoiding small resizes and only downscaling.
- Blacklisting: I have [a post on my site](http://thume.ca/projects/2012/11/14/magic-png-files/) about special trickily-crafted PNG files, I can tell Enfasten not to mess with those and ruin the effect.
- Pass-through images: PNG/JPEG files such as favicons that do not benefit from responsive variants can be copied to their original output paths without hashing, resizing, manifest entries, or optimizer batches.
- Even rewrites relative `img` `src` attributes.
- Supports ordered path-based `sizes` rules when one global `sizes` value cannot describe every image layout.
- Adds intrinsic dimensions to generated WebP `source` elements and their `img` fallbacks so responsive images reserve their layout space before loading.
- Supports `png` and `jpg` files, everything else is left alone.
- Works with any static site generator!

## Example

Below is an example of what the input and output of Enfasten look like. Basically it copies your entire site from an input to an output folder, adding in an images folder with hashed, optimized and resized versions of your images. Then it rewrites all your HTML to reference those. For now the original images are still copied in case they are referenced by RSS, scripts, CSS, etc.

<details>
  <summary>Example input and output directory hierarchy</summary>
<p>


```
test
├── _fastsite
...
│  ├── archive.html
│  ├── assets
│  │  ├── images
│  │  │  ├── 1-bing-4da5feb8-original.png
│  │  │  ├── 1-ddg-eb1bf143-original.png
│  │  │  ├── 1-google-6efffef5-original.png
│  │  │  ├── 1-samuru-93e3f1fc-660px.png
│  │  │  ├── 1-samuru-93e3f1fc-original.png
│  │  │  ├── 2-bing-078cbd23-original.png
│  │  │  ├── 2-ddg-68249286-original.png
│  │  │  ├── 2-google-c8456412-original.png
│  │  │  ├── 2-samuru-c6b17722-660px.png
│  │  │  ├── 2-samuru-c6b17722-original.png
│  │  │  ├── 3-google-caf9e182-original.png
│  │  │  ├── Beowulf-f3168a7d-660px.png
│  │  │  ├── Beowulf-f3168a7d-1320px.png
│  │  │  ├── Beowulf-f3168a7d-original.png
│  │  │  ├── canus-loot-6549ac19-original.jpg
│  │  │  ├── case-6b5e62c5-original.jpg
...
│  │  ├── postassets
...
│  │  │  ├── hackEnglish
│  │  │  │  ├── Beowulf.png
│  │  │  │  ├── Colours-of-Gatsby.png
│  │  │  │  ├── lotf-1.png
│  │  │  │  ├── lotf-2.png
│  │  │  │  └── markov-poster.png
...
│  │  │  ├── keyboardhw
│  │  │  │  ├── canus-loot.jpg
│  │  │  │  ├── case.jpg
...
│  │  │  ├── search
│  │  │  │  ├── 1-bing.png
│  │  │  │  ├── 1-ddg.png
│  │  │  │  ├── 1-google.png
│  │  │  │  ├── 1-samuru.png
│  │  │  │  ├── 2-bing.png
│  │  │  │  ├── 2-ddg.png
│  │  │  │  ├── 2-google.png
│  │  │  │  ├── 2-samuru.png
│  │  │  │  └── 3-google.png
...
├── _site
...
│  ├── archive.html
│  ├── assets
│  │  ├── postassets
...
│  │  │  ├── hackEnglish
│  │  │  │  ├── Beowulf.png
│  │  │  │  ├── Colours-of-Gatsby.png
│  │  │  │  ├── lotf-1.png
│  │  │  │  ├── lotf-2.png
│  │  │  │  └── markov-poster.png
...
│  │  │  ├── keyboardhw
│  │  │  │  ├── canus-loot.jpg
│  │  │  │  ├── case.jpg
...
│  │  │  ├── search
│  │  │  │  ├── 1-bing.png
│  │  │  │  ├── 1-ddg.png
│  │  │  │  ├── 1-google.png
│  │  │  │  ├── 1-samuru.png
│  │  │  │  ├── 2-bing.png
│  │  │  │  ├── 2-ddg.png
│  │  │  │  ├── 2-google.png
│  │  │  │  ├── 2-samuru.png
│  │  │  │  └── 3-google.png
...
├── enfasten.yml
└── enfasten_manifest.yml
```

</p></details>

## Getting Started

On the [releases page](https://github.com/trishume/enfasten/releases) you can download pre-built static binaries for macOS and Linux that you can put somewhere in your `PATH`.

If you have [Go](https://golang.org/) installed you should be able to run:

```bash
$ go get github.com/trishume/enfasten
```

and then make sure your Go `bin` folder is in your `PATH`. Alternatively, clone the repo and run `go get` and then `go install`.

After you've installed enfasten, create an `enfasten.yml` config file in your static site's directory (see "Configuration" section below for an example) and then run:

```bash
# Looks for an enfasten.yml file in the current directory
$ enfasten
# Looks for an enfasten.yml in the specified directory
$ enfasten -basepath my/site/folder
# Transforms one language directory and defers other languages' new WebP work
$ enfasten -lang en
# Runs with culling, only do this once all your images are optimized
$ enfasten -cull
```

## Configuration

Enfasten is configred through an `enfasten.yml` file. All keys are optional, here's a good basic config file for a Jekyll site that is a static 660px wide:

```yaml
# Jekyll by default outputs to _site, we'll put our result in _fastsite
inputfolder: _site
outputfolder: _fastsite
sizesattr: 660px
# Optional responsive-density ceiling. Zero disables it. Enfasten preserves
# normal selection at or below the ceiling and adds resolution-aware sizes
# hints for common fractional and integer display densities through 4x.
maxdpr: 0
# Normal and retina resolutions:
widths: [660,1320]
# ImagOptim is a great optimizer for macOS, here's how to connect it:
optimcommand: ['open', '-a', 'ImageOptim']
# Sometimes there's files we don't want to bother processing and rewriting
blacklist:
  - favicon.png
```

And here's the full slate of config options, the default values and documentation of what they do:

```yaml
# The folder to take files and images from, relative to enfasten.yml
inputfolder: _site
# The folder to put output in, relative to enfasten.yml
outputfolder: _fastsite
# Optional source-only fragment folder name. For each include directive,
# Enfasten searches beside the source page and then each parent through the
# input root. Every matching folder is excluded from the output.
includefolder: includes
# The folder to put all images in, relative to outputfolder
imagefolder: assets/images
# PNG/JPEG globs relative to inputfolder that should be copied normally but not
# processed as responsive images or added to the manifest/optimizer queue.
passthroughimages: []
# PNG/JPEG globs relative to inputfolder that remain eligible for WebP during
# a language-filtered build because every locale shares them.
sharedimages: []
# The file name for the manifest, relative to enfasten.yml If this is set to the
# empty string, no manifest will be used.
manifestfile: enfasten_manifest.yml
# The contents of the "sizes" attribute for responsive image tags, if this is
# the empty string the attribute will be omitted.
sizesattr: ""
# Optional ordered rules for layout-specific sizes values and media gates. Patterns are
# doublestar globs matched against image paths relative to inputfolder. The
# first match wins, then sizesattr is used as the fallback. A sizes attribute
# already present on the source img takes precedence over both. Optional widths
# add image variants only for files matching that rule. Optional media prevents
# all real candidates from loading when its media query does not match. Set webp
# to true to generate a WebP source for matching images when webpcommand is
# configured.
sizesrules:
  - pattern: '**/images/screenshots/*.png'
    sizes: '(min-width: 60em) 30em, 90vw'
    media: '(min-width: 40em)'
    widths: [2200]
    webp: true
# An array of strings specifying a command and arguments to run to optimize
# images. If non-null, Enfasten will append all the files needing optimization to
# this, run it and wait for it to finish.
optimcommand: null
# Maximum number of new image paths passed to each optimizer invocation. This
# avoids oversized commands and makes a failing batch easier to retry. Set to 0
# to pass every new image in one command.
optimbatchsize: 32
# Optional WebP encoder command. For each responsive source image, Enfasten
# appends the input path and `-o <output path>` and writes a picture/source
# element ahead of the original-format img fallback.
webpcommand: null
# Whether to copy/transform non-image files into the output. Set this to false if
# you're only using Enfasten as an image resizing tool and parsing the generated
# manifest with your own script.
docopy: true
# The threshold of scaling above which Enfasten won't bother. In this case if the
# destination width is greater than 0.9 times the source width, that size won't be
# created.
scalethreshold: 0.9
# Same as above, but for .jpg files. Separate because re-encoding is bad.
jpgscalethreshold: 0.7
# The quality with which to re-encode JPG files, higher is larger but less lossy.
jpgquality: 90
# The array of widths to which Enfasten will try and downscale each image
widths: []
# An array of Go file glob patterns relative to inputfolder of files not to process
blacklist: []
```

An individual source image can override the global ceiling with the build-only
`data-enfasten-max-dpr` attribute. Enfasten consumes the attribute and does not
copy it to generated HTML:

```html
<img src="images/hero.png" data-enfasten-max-dpr="2">
<img src="images/detail.png" data-enfasten-max-dpr="3">
<img src="images/original-density.png" data-enfasten-max-dpr="none">
```

`maxdpr` requires an explicit responsive `sizes` value, either on the source
image, from a matching `sizesrules` entry, or from `sizesattr`. It changes only
the browser's generated source-size hints; CSS layout dimensions are unchanged.

An optional `media` value on a `sizesrules` entry prevents the real image from
loading unless that media query matches. This is useful when separate image
elements provide genuinely different mobile and desktop compositions. Enfasten
emits media-gated modern and original-format `source` elements plus a transparent
data-URL `img` fallback, so a nonmatching alternative produces no network image
request.
Unlike `sizes`, which chooses among candidates after the browser decides to load
an image, `media` controls whether those candidates are eligible at all.

The build-only `data-enfasten-media` attribute overrides a matching rule for one
image occurrence. Use `none` to disable a rule's gate; Enfasten consumes the
attribute in generated HTML:

```html
<img src="images/mobile-hero.png" data-enfasten-media="(max-width: 39.9375em)">
<img src="images/desktop-hero.png" data-enfasten-media="(min-width: 40em)">
<img src="images/always-visible.png" data-enfasten-media="none">
```

**Note:** Enfasten detects changes to configured responsive widths and the WebP command. Other image-processing option changes such as `jpgquality` and `scalethreshold` may require deleting the manifest and processed images so they are rebuilt.

## The Manifest

Enfasten can output an `enfasten_manifest.yml` file that describes all the images it knows about and has built. You can delete this file and for the most part Enfasten will do the exact same thing it would have done otherwise, but slower. The manifest provides the following benefits:

- **Makes builds faster**: Without a manifest, Enfasten has to load all your image files, parse them to figure out their size, then realize there's already an image like that and skip it. This takes about `1.3s` on my site. With the manifest, Enfasten only has to load files and hash them, which only takes about `0.3`s on my site.
- **Enables culling**: Culling is a feature where Enfasten can detect smaller dimension images with larger file sizes and delete them. Without the manifest, it can't remember that it did this so if you try and use culling it will immediately re-generate and optimize those images again.
- **Parse it yourself**: If you want you can also parse this file yourself and use it in your own build steps. There's even an option in Enfasten's config file to not do the copy-and-transform stage of the process so that you can use Enfasten just for its optimizing and resizing and do the rewriting yourself. This allows you to use Enfasten with a dynamic web app.

## Roadmap

This was a weekend project, there's a few things I haven't got around to yet. If you want to see these features, I welcome contributions!

- [ ] Option to not copy files from original locations. If you have your site and blacklist set up right the extra files just bloat the output.
- [ ] Better error messages. Right now I just bubble up Go errors everywhere.
- [ ] More bandwidth optimizations to make your site faster:
    - [ ] HTML minimization
    - [ ] CSS minimization
    - [ ] JS minimization
    - [ ] SVG optimization
- [ ] Generate service workers and set them up to preload and cache things for extra speed!

## Where the heck did the name come from?

It sounded like a word that would mean "to make fast". It also sounds like [Emscripten](https://github.com/kripken/emscripten) which is another project about making the web fast, although in a totally different and unrelated way. In my head I alternate between pronouncing it "En-fast-en" and "En-fassen". I also Googled it and it didn't look like it collided with anything too important.

## License

This project is released under the Apache license and was written by [Tristan Hume](http://thume.ca/)
