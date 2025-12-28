package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	CAKey = "ca.crt"
)

type PEMFile struct {
	Filename string
	Content  []byte
}

func DownloadPEMBundles(ctx context.Context, baseURL string) ([]PEMFile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list bundles: %s", resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	var pemFiles []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" && (strings.HasSuffix(a.Val, ".pem") || strings.HasSuffix(a.Val, ".crt")) {
					pemFiles = append(pemFiles, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	var results []PEMFile

	for _, name := range pemFiles {
		// u := base.ResolveReference(&url.URL{Path: name})
		url, _ := url.JoinPath(baseURL, name)
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		data, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return nil, err
		}

		results = append(results, PEMFile{
			Filename: name,
			Content:  data,
		})
	}

	return results, nil
}

func (r *CABundleReconciler) checkConfigMap(ctx context.Context, bundle PEMFile) bool {
	// Placeholder for checking if the ConfigMap exists and contains the bundle
	cm := &corev1.ConfigMap{}

	cmName := r.reName(bundle.Filename)

	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: r.TargetNamespace}, cm)
	if err != nil {
		return false
	}

	if existingBundle, exists := cm.Data[bundle.Filename]; exists && existingBundle == string(bundle.Content) {
		return true
	}
	return false

}

func (r *CABundleReconciler) createOrUpdateConfigMap(ctx context.Context, bundle PEMFile) error {
	logger := logf.FromContext(ctx)
	cm := &corev1.ConfigMap{}

	cmName := r.reName(bundle.Filename)
	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: r.TargetNamespace}, cm)

	if apierrors.IsNotFound(err) {
		// Create new ConfigMap if it doesn't exist
		logger.Info("Creating ConfigMap", "name", cmName, "namespace", r.TargetNamespace)
		cm = &corev1.ConfigMap{
			ObjectMeta: ctrl.ObjectMeta{
				Name:      cmName,
				Namespace: r.TargetNamespace,
				Labels: map[string]string{
					"app": "cabundle-operator",
				},
			},
			Data: map[string]string{
				CAKey: string(bundle.Content),
			},
		}
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	// Update existing ConfigMap
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[CAKey] = string(bundle.Content)
	return r.Update(ctx, cm)
}

func (r *CABundleReconciler) GetBundleConfigMaps(ctx context.Context) ([]string, error) {
	logger := logf.FromContext(ctx)
	cmList := &corev1.ConfigMapList{}
	err := r.List(ctx, cmList, client.InNamespace(r.TargetNamespace), client.MatchingLabels{"app": "cabundle-operator"})
	if err != nil {
		logger.Error(err, "unable to list ConfigMaps")
		return nil, err
	}

	var bundleCMNames []string
	for _, cm := range cmList.Items {
		bundleCMNames = append(bundleCMNames, cm.Name)
	}

	return bundleCMNames, nil
}

func (r *CABundleReconciler) DeleteBundleConfigMap(ctx context.Context, name string) error {
	logger := logf.FromContext(ctx)
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: r.TargetNamespace}, cm)
	if err != nil {
		logger.Error(err, "unable to fetch ConfigMap for deletion", "name", name)
		return client.IgnoreNotFound(err)
	}

	logger.Info("Deleting stale ConfigMap", "name", name)
	return r.Delete(ctx, cm)
}

func (r *CABundleReconciler) CleanUpConfigMaps(ctx context.Context, bundles []PEMFile) error {
	logger := logf.FromContext(ctx)
	logger.Info("Starting cleanup of stale ConfigMaps")

	bundleCMNames, err := r.GetBundleConfigMaps(ctx)
	if err != nil {
		return err
	}

	existingBundles := make(map[string]bool)
	for _, b := range bundleCMNames {
		existingBundles[b] = false
	}
	for _, b := range bundles {
		cmName := r.reName(b.Filename)
		if _, exists := existingBundles[cmName]; exists {
			existingBundles[cmName] = true
		}
	}

	for cmName, found := range existingBundles {
		if !found {
			logger.Info("Found stale ConfigMap to delete", "name", cmName)
			err := r.DeleteBundleConfigMap(ctx, cmName)
			if err != nil {
				return err
			}
		}
	}

	logger.Info("Cleanup of stale ConfigMaps completed")

	return nil
}

func (r *CABundleReconciler) reName(name string) string {
	nameTrimmed := strings.TrimSuffix(name, ".pem")
	nameTrimmed = strings.TrimSuffix(nameTrimmed, ".crt")
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	return strings.ToLower(re.ReplaceAllString(nameTrimmed, "-"))
}
