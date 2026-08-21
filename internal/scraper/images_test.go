package scraper

import "testing"

// Mirrors a post's lightbox: the cover, the work's own pictures, then the
// per-part art, each anchor empty and the picture only in its href. The
// thumbnails around it repeat two of them and carry a placeholder in src.
const galleryPage = `
<div class="fotorama">
  <a id="img_cover" href="https://pic.example.xyz/RJ01234567_img_main.jpg"></a>
  <a id="img_1" href="https://pic1.example.xyz/RJ01234567(1).jpg"></a>
  <a id="img_2" href="https://pic1.example.xyz/RJ01234567(2).jpg"></a>
  <a id="img_work_parts_1" href="https://img.example.xyz/images/8ad511c7.jpg"></a>
  <a id="img_cover" href="https://pic.example.xyz/RJ01234567_img_main.jpg"></a>
  <a id="buy" href="https://www.dlsite.com/work/RJ01234567.html"></a>
  <a id="script" href="javascript:void(0)"></a>
</div>
<img class="lazy" src="data:image/svg+xml,%3Csvg%3E%3C/svg%3E" data-src="https://pic.example.xyz/RJ01234567_img_main.jpg"/>
<img class="lazy" src="data:image/svg+xml,%3Csvg%3E%3C/svg%3E" data-src="https://img.example.xyz/images/8ad511c7.jpg"/>`

func TestExtractImagesKeepsGalleryOrderAndDropsNonPictures(t *testing.T) {
	doc, base := parse(t, galleryPage)
	images := extractImages(doc, base)

	want := []string{
		"https://pic.example.xyz/RJ01234567_img_main.jpg",
		"https://pic1.example.xyz/RJ01234567(1).jpg",
		"https://pic1.example.xyz/RJ01234567(2).jpg",
		"https://img.example.xyz/images/8ad511c7.jpg",
	}
	if len(images) != len(want) {
		t.Fatalf("got %d images, want %d: %v", len(images), len(want), images)
	}
	for i, w := range want {
		if images[i] != w {
			t.Errorf("image %d = %q, want %q", i, images[i], w)
		}
	}
}

// A post that never opened a lightbox still has its cover, read elsewhere.
func TestExtractImagesIgnoresPageChrome(t *testing.T) {
	doc, base := parse(t, `
<a href="https://japaneseasmr.com/logo.png">home</a>
<img class="lazy" data-src="https://pic.example.xyz/RJ123456_img_main.jpg" alt="Random post"/>`)

	if images := extractImages(doc, base); len(images) != 0 {
		t.Fatalf("got %v, want nothing outside the gallery", images)
	}
}
